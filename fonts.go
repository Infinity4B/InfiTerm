package main

import (
	"encoding/binary"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

// 本机已安装字体的族名列表，供设置里的字体搜索框使用。
// 直接解析字体文件的 name/post/OS2 表，不需要浏览器的本地字体权限
type fontInfo struct {
	Family string `json:"family"`
	Mono   bool   `json:"mono"`
}

var fontCache struct {
	sync.Mutex
	at   time.Time
	list []fontInfo
}

func handleFonts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(listFonts())
}

func listFonts() []fontInfo {
	fontCache.Lock()
	defer fontCache.Unlock()
	if fontCache.list != nil && time.Since(fontCache.at) < 2*time.Minute {
		return fontCache.list
	}
	families := map[string]bool{}
	dirs := []string{"/System/Library/Fonts", "/Library/Fonts", filepath.Join(homeDir(), "Library", "Fonts")}
	// 苹方等按需下载的系统字体
	assets, _ := filepath.Glob("/System/Library/AssetsV2/com_apple_MobileAsset_Font*")
	dirs = append(dirs, assets...)
	for _, dir := range dirs {
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			switch strings.ToLower(filepath.Ext(p)) {
			case ".ttf", ".otf", ".ttc", ".otc":
				readFontFile(p, families)
			}
			return nil
		})
	}
	list := make([]fontInfo, 0, len(families))
	for fam, mono := range families {
		// 以 . 开头的是系统私有字体（如 .SF NS），LastResort 是兜底字体，都不该给用户选
		if fam == "" || strings.HasPrefix(fam, ".") || fam == "LastResort" {
			continue
		}
		list = append(list, fontInfo{Family: fam, Mono: mono || looksMono(fam)})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Mono != list[j].Mono {
			return list[i].Mono
		}
		return strings.ToLower(list[i].Family) < strings.ToLower(list[j].Family)
	})
	fontCache.list, fontCache.at = list, time.Now()
	return list
}

func looksMono(fam string) bool {
	f := strings.ToLower(fam)
	for _, k := range []string{"mono", "code", "courier", "menlo", "consol", "terminal", "nerd font"} {
		if strings.Contains(f, k) {
			return true
		}
	}
	return false
}

func readFontFile(path string, out map[string]bool) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	var hdr [12]byte
	if _, err := f.ReadAt(hdr[:], 0); err != nil {
		return
	}
	offsets := []int64{0}
	if string(hdr[:4]) == "ttcf" {
		n := binary.BigEndian.Uint32(hdr[8:12])
		if n > 64 {
			n = 64
		}
		buf := make([]byte, 4*n)
		if _, err := f.ReadAt(buf, 12); err != nil {
			return
		}
		offsets = offsets[:0]
		for i := uint32(0); i < n; i++ {
			offsets = append(offsets, int64(binary.BigEndian.Uint32(buf[i*4:])))
		}
	}
	for _, off := range offsets {
		if fam, mono, ok := readFace(f, off); ok {
			out[fam] = out[fam] || mono
		}
	}
}

func readFace(f *os.File, off int64) (family string, mono bool, ok bool) {
	var h [12]byte
	if _, err := f.ReadAt(h[:], off); err != nil {
		return
	}
	num := int(binary.BigEndian.Uint16(h[4:6]))
	if num == 0 || num > 200 {
		return
	}
	recs := make([]byte, 16*num)
	if _, err := f.ReadAt(recs, off+12); err != nil {
		return
	}
	type table struct{ off, len uint32 }
	tables := map[string]table{}
	for i := 0; i < num; i++ {
		r := recs[i*16:]
		tables[string(r[:4])] = table{binary.BigEndian.Uint32(r[8:12]), binary.BigEndian.Uint32(r[12:16])}
	}
	name, has := tables["name"]
	if !has || name.len < 6 || name.len > 1<<20 {
		return
	}
	data := make([]byte, name.len)
	if _, err := f.ReadAt(data, int64(name.off)); err != nil {
		return
	}
	family = parseFamily(data)
	if post, has := tables["post"]; has && post.len >= 16 {
		var b [4]byte
		if _, err := f.ReadAt(b[:], int64(post.off)+12); err == nil {
			mono = binary.BigEndian.Uint32(b[:]) != 0 // isFixedPitch
		}
	}
	if os2, has := tables["OS/2"]; has && os2.len >= 42 && !mono {
		var b [4]byte
		if _, err := f.ReadAt(b[:], int64(os2.off)+32); err == nil {
			mono = b[0] == 2 && b[3] == 9 // PANOSE：拉丁文字、等宽比例
		}
	}
	return family, mono, family != ""
}

// 优先英文名，其次排版族名（nameID 16），再次普通族名（nameID 1）
func parseFamily(data []byte) string {
	count := int(binary.BigEndian.Uint16(data[2:4]))
	strOff := int(binary.BigEndian.Uint16(data[4:6]))
	best, bestScore := "", -1
	for i := 0; i < count; i++ {
		p := 6 + i*12
		if p+12 > len(data) {
			break
		}
		r := data[p : p+12]
		platform := binary.BigEndian.Uint16(r[0:2])
		encoding := binary.BigEndian.Uint16(r[2:4])
		lang := binary.BigEndian.Uint16(r[4:6])
		nameID := binary.BigEndian.Uint16(r[6:8])
		length := int(binary.BigEndian.Uint16(r[8:10]))
		start := strOff + int(binary.BigEndian.Uint16(r[10:12]))
		if (nameID != 1 && nameID != 16) || start+length > len(data) {
			continue
		}
		score := 0
		var s string
		switch {
		case platform == 3 || platform == 0:
			b := data[start : start+length]
			u := make([]uint16, len(b)/2)
			for k := range u {
				u[k] = binary.BigEndian.Uint16(b[k*2:])
			}
			s = string(utf16.Decode(u))
			if platform == 0 || lang == 0x409 {
				score += 20
			} else {
				score += 5
			}
		case platform == 1 && encoding == 0:
			s = string(data[start : start+length]) // Mac Roman，族名基本都是 ASCII
			if lang == 0 {
				score += 18
			}
		default:
			continue
		}
		if nameID == 16 {
			score += 10
		}
		if s = strings.TrimSpace(s); s != "" && score > bestScore {
			best, bestScore = s, score
		}
	}
	return best
}
