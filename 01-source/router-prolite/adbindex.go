package main

import (
	"bytes"
	"compress/flate"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

const (
	adbTypeMask  = 0xf0000000
	adbValueMask = 0x0fffffff
)

// adbDecompress 按 ADB 文件头第 4 字节识别压缩方式：
// '.' 不压缩，'d' 原始 DEFLATE，'c' 后跟算法/级别（0 无、1 Deflate、2 Zstd）。
func adbDecompress(raw []byte) ([]byte, error) {
	if len(raw) < 4 || string(raw[:3]) != "ADB" {
		return nil, fmt.Errorf("不是 ADB 文件")
	}
	var body []byte
	switch raw[3] {
	case '.':
		body = raw
	case 'd':
		zr := flate.NewReader(bytes.NewReader(raw[4:]))
		defer zr.Close()
		b, err := io.ReadAll(io.LimitReader(zr, 128*1024*1024))
		if err != nil {
			return nil, fmt.Errorf("ADB deflate 解压失败：%w", err)
		}
		body = b
	case 'c':
		if len(raw) < 6 {
			return nil, fmt.Errorf("ADB 压缩头不完整")
		}
		alg, level := raw[4], raw[5]
		switch alg {
		case 0:
			body = raw[6:]
		case 1:
			zr := flate.NewReader(bytes.NewReader(raw[6:]))
			defer zr.Close()
			b, err := io.ReadAll(io.LimitReader(zr, 128*1024*1024))
			if err != nil {
				return nil, fmt.Errorf("ADB deflate 解压失败：%w", err)
			}
			body = b
		case 2:
			return nil, fmt.Errorf("ADB 使用 zstd 压缩（level %d），当前构建未启用 zstd 支持", level)
		default:
			return nil, fmt.Errorf("未知 ADB 压缩算法 %d", alg)
		}
	default:
		return nil, fmt.Errorf("未知 ADB 魔数 %q", raw[:4])
	}
	if len(body) < 8 || string(body[:4]) != "ADB." {
		return nil, fmt.Errorf("ADB 内容头无效")
	}
	return body, nil
}

type adbBlock struct {
	typ     uint32
	payload []byte
}

func adbReadBlocks(body []byte) ([]adbBlock, error) {
	if len(body) < 8 {
		return nil, fmt.Errorf("ADB 内容过短")
	}
	off := 8 // ADB. + schema
	out := []adbBlock{}
	for off+4 <= len(body) {
		v := binary.LittleEndian.Uint32(body[off:])
		var typ uint32
		var raw uint64
		hdr := 4
		if v>>30 == 3 {
			if off+16 > len(body) {
				return nil, fmt.Errorf("ADB 扩展块头不完整")
			}
			typ = v & 0x3fffffff
			raw = binary.LittleEndian.Uint64(body[off+8:])
			hdr = 16
		} else {
			typ = uint32(v >> 30)
			raw = uint64(v & 0x3fffffff)
		}
		if raw < uint64(hdr) || uint64(off)+raw > uint64(len(body)) {
			return nil, fmt.Errorf("ADB 块越界（offset=%d raw=%d len=%d）", off, raw, len(body))
		}
		out = append(out, adbBlock{typ: typ, payload: body[off+hdr : off+int(raw)]})
		size := (raw + 7) &^ 7
		if size == 0 {
			break
		}
		off += int(size)
	}
	return out, nil
}

type adbParser struct {
	data []byte
}

func (p *adbParser) u32(off int) uint32 {
	if off < 0 || off+4 > len(p.data) {
		return 0
	}
	return binary.LittleEndian.Uint32(p.data[off:])
}

func (p *adbParser) u64(off int) uint64 {
	if off < 0 || off+8 > len(p.data) {
		return 0
	}
	return binary.LittleEndian.Uint64(p.data[off:])
}

// obj 读取 OBJECT/ARRAY：u32 计数（含计数自身）+ 后续 adb_val_t。
func (p *adbParser) obj(v uint32) []uint32 {
	if v == 0 {
		return nil
	}
	off := int(v & adbValueMask)
	if off < 0 || off+4 > len(p.data) {
		return nil
	}
	n := p.u32(off)
	if n == 0 || n > 1<<20 {
		return nil
	}
	out := make([]uint32, 0, n-1)
	for i := uint32(1); i < n; i++ {
		pos := off + 4*int(i)
		if pos+4 > len(p.data) {
			break
		}
		out = append(out, p.u32(pos))
	}
	return out
}

func (p *adbParser) blob(v uint32) []byte {
	t := v & adbTypeMask
	off := int(v & adbValueMask)
	switch t {
	case 0x80000000:
		if off+1 > len(p.data) {
			return nil
		}
		n := int(p.data[off])
		if off+1+n > len(p.data) {
			return nil
		}
		return p.data[off+1 : off+1+n]
	case 0x90000000:
		if off+2 > len(p.data) {
			return nil
		}
		n := int(binary.LittleEndian.Uint16(p.data[off:]))
		if off+2+n > len(p.data) {
			return nil
		}
		return p.data[off+2 : off+2+n]
	case 0xa0000000:
		if off+4 > len(p.data) {
			return nil
		}
		n := int(p.u32(off))
		if n < 0 || off+4+n > len(p.data) {
			return nil
		}
		return p.data[off+4 : off+4+n]
	case 0x10000000:
		return []byte(strconv.FormatInt(int64(v&adbValueMask), 10))
	}
	return nil
}

func (p *adbParser) str(v uint32) string {
	return string(p.blob(v))
}

func (p *adbParser) intVal(v uint32) int64 {
	switch v & adbTypeMask {
	case 0x10000000, 0x00000000:
		return int64(v & adbValueMask)
	case 0x20000000:
		return int64(p.u32(int(v & adbValueMask)))
	case 0x30000000:
		return int64(p.u64(int(v & adbValueMask)))
	}
	return 0
}

func (p *adbParser) deps(v uint32) []string {
	if v == 0 {
		return nil
	}
	out := []string{}
	for _, e := range p.obj(v) {
		f := p.obj(e)
		if len(f) == 0 {
			continue
		}
		name := p.str(f[0])
		if name == "" {
			continue
		}
		ver := ""
		if len(f) > 1 {
			ver = p.str(f[1])
		}
		if ver == "" {
			out = append(out, name)
			continue
		}
		match := int64(0)
		if len(f) > 2 {
			match = p.intVal(f[2])
		}
		op := adbMatchOp(match)
		if op == "" {
			out = append(out, name)
		} else {
			out = append(out, name+op+ver)
		}
	}
	return out
}

func adbMatchOp(m int64) string {
	switch m & 0x0f {
	case 0, 1:
		return "="
	case 2:
		return "<"
	case 3:
		return "<="
	case 4:
		return ">"
	case 5:
		return ">="
	case 8, 9:
		return "="
	case 11:
		return "<="
	case 13:
		return ">="
	case 6, 7:
		return ""
	}
	return "="
}

// parseADBIndex 解析 apk v3 的 ADB index（ADB.indx）。
func parseADBIndex(raw []byte, feed Feed) ([]*IndexPkg, string, error) {
	body, err := adbDecompress(raw)
	if err != nil {
		return nil, "", err
	}
	if string(body[4:8]) != "indx" {
		return nil, "", fmt.Errorf("ADB schema 不是 indx：%q", body[4:8])
	}
	blocks, err := adbReadBlocks(body)
	if err != nil {
		return nil, "", err
	}
	var payload []byte
	sigBlocks := 0
	for _, b := range blocks {
		switch b.typ {
		case 0:
			if payload == nil {
				payload = b.payload
			}
		case 1:
			sigBlocks++
		}
	}
	if len(payload) < 8 {
		return nil, "", fmt.Errorf("ADB index 缺少 ADB 元数据块")
	}
	p := &adbParser{data: payload}
	root := p.u32(4)
	rootFields := p.obj(root)
	if len(rootFields) < 2 {
		return nil, "", fmt.Errorf("ADB index 根对象字段不足")
	}
	pkgRefs := p.obj(rootFields[1])
	if len(pkgRefs) == 0 {
		return nil, "", fmt.Errorf("ADB index 包列表为空")
	}

	base := strings.TrimRight(feed.URL, "/")
	out := make([]*IndexPkg, 0, len(pkgRefs))
	for _, ref := range pkgRefs {
		f := p.obj(ref)
		if len(f) < 2 {
			continue
		}
		name := p.str(f[0])
		version := p.str(f[1])
		if name == "" || version == "" {
			continue
		}
		pkg := &IndexPkg{
			Name:     name,
			Version:  version,
			Feed:     base,
			URL:      base + "/" + name + "-" + version + ".apk",
			Filename: name + "-" + version + ".apk",
		}
		if len(f) > 2 {
			hashes := p.blob(f[2])
			if len(hashes) > 0 {
				pkg.Checksum = hex.EncodeToString(hashes)
				// ADB index 的 hashes 是包内 ADB 元数据块 payload 的 SHA256，
				// 不是整个 .apk 文件的 SHA256；下载后需按块校验。
				pkg.ChecksumKind = "adb-payload"
			}
		}
		if len(f) > 3 {
			pkg.Description = p.str(f[3])
		}
		if len(f) > 4 {
			pkg.Arch = p.str(f[4])
		}
		if len(f) > 5 {
			pkg.License = p.str(f[5])
		}
		if len(f) > 6 {
			pkg.Origin = p.str(f[6])
		}
		if len(f) > 8 {
			pkg.Homepage = p.str(f[8])
		}
		if len(f) > 11 {
			pkg.InstalledSize = p.intVal(f[11])
		}
		if len(f) > 12 {
			pkg.Size = p.intVal(f[12])
		}
		if len(f) > 14 {
			pkg.Depends = p.deps(f[14])
		}
		if len(f) > 15 {
			pkg.Provides = p.deps(f[15])
		}
		if len(f) > 16 {
			pkg.Replaces = p.deps(f[16])
		}
		out = append(out, pkg)
	}
	note := "apk v3 ADB 索引"
	if sigBlocks > 0 {
		note += "；检测到 ADB 签名块（ECDSA/PEM），本次只读解析未验签"
	}
	return out, note, nil
}

// adbPackagePayloadSHA256 计算 apk v3 包内 ADB 元数据块 payload 的 SHA256。
// ADB index 的 hashes 字段对应这个值，而不是整个 .apk 文件。
func adbPackagePayloadSHA256(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(raw) < 4 || string(raw[:3]) != "ADB" {
		return "", fmt.Errorf("不是 apk v3 ADB 包")
	}
	body, err := adbDecompress(raw)
	if err != nil {
		return "", err
	}
	blocks, err := adbReadBlocks(body)
	if err != nil {
		return "", err
	}
	for _, b := range blocks {
		if b.typ == 0 {
			sum := sha256.Sum256(b.payload)
			return hex.EncodeToString(sum[:]), nil
		}
	}
	return "", fmt.Errorf("包内没有 ADB 元数据块")
}

func adbIndexURL(raw string) (indexURL, base string, hasADB bool) {
	u := strings.TrimSpace(strings.TrimRight(raw, "/"))
	if u == "" {
		return "", "", false
	}
	low := strings.ToLower(u)
	if strings.HasSuffix(low, ".adb") {
		if i := strings.LastIndex(u, "/"); i > 0 {
			return u, u[:i], true
		}
		return "", "", false
	}
	if strings.HasSuffix(low, ".tar.gz") || strings.HasSuffix(low, ".gz") ||
		strings.HasSuffix(low, "/packages") || strings.HasSuffix(low, ".apk") {
		if i := strings.LastIndex(u, "/"); i > 0 {
			return "", u[:i], false
		}
		return "", "", false
	}
	return u + "/packages.adb", u, true
}

// expandRepoVars 展开仓库地址中的 $arch / ${arch}。
func expandRepoVars(url, arch string) string {
	if arch == "" {
		return url
	}
	url = strings.ReplaceAll(url, "${arch}", arch)
	url = strings.ReplaceAll(url, "$arch", arch)
	return url
}
