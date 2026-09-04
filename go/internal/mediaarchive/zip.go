package mediaarchive

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"hash/crc32"
)

// writeZipEntry 用预计算 CRC/大小写入 Deflate 条目，mtime 固定 1980-01-01，保证重复导出哈希稳定。
func writeZipEntry(zw *zip.Writer, name string, data []byte) error {
	var compressed bytes.Buffer
	fw, err := flate.NewWriter(&compressed, 9)
	if err != nil {
		return err
	}
	if _, err := fw.Write(data); err != nil {
		return err
	}
	if err := fw.Close(); err != nil {
		return err
	}
	header := &zip.FileHeader{
		Name:               name,
		Method:             zip.Deflate,
		CreatorVersion:     (3 << 8) | 20,
		ReaderVersion:      20,
		ExternalAttrs:      0o600 << 16,
		ModifiedTime:       0,
		ModifiedDate:       0x0021, // 1980-01-01
		CRC32:              crc32.ChecksumIEEE(data),
		CompressedSize64:   uint64(compressed.Len()),
		UncompressedSize64: uint64(len(data)),
	}
	if zipNameNeedsUTF8(name) {
		header.Flags |= 0x800
	}
	w, err := zw.CreateRaw(header)
	if err != nil {
		return err
	}
	_, err = w.Write(compressed.Bytes())
	return err
}

func zipNameNeedsUTF8(name string) bool {
	for i := 0; i < len(name); i++ {
		if name[i] >= 0x80 {
			return true
		}
	}
	return false
}
