// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// 4:4:4 JPEG encode adapted from Go image/jpeg (no chroma downsample).

package delivery

import (
	"bufio"
	"image"
	"image/color"
	"io"
)

const (
	jpegSOF0Marker = 0xc0
	jpegDHTMarker  = 0xc4
	jpegDQTMarker  = 0xdb
	jpegBlockSize  = 8 * 8
)

type jpegBlock [jpegBlockSize]int32

func jpegDiv(a, b int32) int32 {
	if a >= 0 {
		return (a + (b >> 1)) / b
	}
	return -((-a + (b >> 1)) / b)
}

var jpegBitCount = [256]byte{
	0, 1, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 4, 4, 4, 4,
	5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5,
	6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6,
	6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6, 6,
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
	7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
	8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8,
}

var unzig = [jpegBlockSize]int{
	0, 1, 8, 16, 9, 2, 3, 10,
	17, 24, 32, 25, 18, 11, 4, 5,
	12, 19, 26, 33, 40, 48, 41, 34,
	27, 20, 13, 6, 7, 14, 21, 28,
	35, 42, 49, 56, 57, 50, 43, 36,
	29, 22, 15, 23, 30, 37, 44, 51,
	58, 59, 52, 45, 38, 31, 39, 46,
	53, 60, 61, 54, 47, 55, 62, 63,
}

type quantIndex int

const (
	quantIndexLuminance quantIndex = iota
	quantIndexChrominance
	nQuantIndex
)

// unscaledQuant are the unscaled quantization tables in zig-zag order. Each
// encoder copies and scales the tables according to its quality parameter.
// The values are derived from section K.1 of the spec, after converting from
// natural to zig-zag order.
var unscaledQuant = [nQuantIndex][jpegBlockSize]byte{
	// Luminance.
	{
		16, 11, 12, 14, 12, 10, 16, 14,
		13, 14, 18, 17, 16, 19, 24, 40,
		26, 24, 22, 22, 24, 49, 35, 37,
		29, 40, 58, 51, 61, 60, 57, 51,
		56, 55, 64, 72, 92, 78, 64, 68,
		87, 69, 55, 56, 80, 109, 81, 87,
		95, 98, 103, 104, 103, 62, 77, 113,
		121, 112, 100, 120, 92, 101, 103, 99,
	},
	// Chrominance.
	{
		17, 18, 18, 24, 21, 24, 47, 26,
		26, 47, 99, 66, 56, 66, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
		99, 99, 99, 99, 99, 99, 99, 99,
	},
}

type huffIndex int

const (
	huffIndexLuminanceDC huffIndex = iota
	huffIndexLuminanceAC
	huffIndexChrominanceDC
	huffIndexChrominanceAC
	nHuffIndex
)

// huffmanSpec specifies a Huffman encoding.
type huffmanSpec struct {
	// count[i] is the number of codes of length i+1 bits.
	count [16]byte
	// value[i] is the decoded value of the i'th codeword.
	value []byte
}

// theHuffmanSpec is the Huffman encoding specifications.
//
// This encoder uses the same Huffman encoding for all images. It is also the
// same Huffman encoding used by section K.3 of the spec.
//
// The DC tables have 12 decoded values, called categories.
//
// The AC tables have 162 decoded values: bytes that pack a 4-bit Run and a
// 4-bit Size. There are 16 valid Runs and 10 valid Sizes, plus two special R|S
// cases: 0|0 (meaning EOB) and F|0 (meaning ZRL).
var theHuffmanSpec = [nHuffIndex]huffmanSpec{
	// Luminance DC.
	{
		[16]byte{0, 1, 5, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0},
		[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
	},
	// Luminance AC.
	{
		[16]byte{0, 2, 1, 3, 3, 2, 4, 3, 5, 5, 4, 4, 0, 0, 1, 125},
		[]byte{
			0x01, 0x02, 0x03, 0x00, 0x04, 0x11, 0x05, 0x12,
			0x21, 0x31, 0x41, 0x06, 0x13, 0x51, 0x61, 0x07,
			0x22, 0x71, 0x14, 0x32, 0x81, 0x91, 0xa1, 0x08,
			0x23, 0x42, 0xb1, 0xc1, 0x15, 0x52, 0xd1, 0xf0,
			0x24, 0x33, 0x62, 0x72, 0x82, 0x09, 0x0a, 0x16,
			0x17, 0x18, 0x19, 0x1a, 0x25, 0x26, 0x27, 0x28,
			0x29, 0x2a, 0x34, 0x35, 0x36, 0x37, 0x38, 0x39,
			0x3a, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48, 0x49,
			0x4a, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58, 0x59,
			0x5a, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68, 0x69,
			0x6a, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78, 0x79,
			0x7a, 0x83, 0x84, 0x85, 0x86, 0x87, 0x88, 0x89,
			0x8a, 0x92, 0x93, 0x94, 0x95, 0x96, 0x97, 0x98,
			0x99, 0x9a, 0xa2, 0xa3, 0xa4, 0xa5, 0xa6, 0xa7,
			0xa8, 0xa9, 0xaa, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6,
			0xb7, 0xb8, 0xb9, 0xba, 0xc2, 0xc3, 0xc4, 0xc5,
			0xc6, 0xc7, 0xc8, 0xc9, 0xca, 0xd2, 0xd3, 0xd4,
			0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda, 0xe1, 0xe2,
			0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8, 0xe9, 0xea,
			0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8,
			0xf9, 0xfa,
		},
	},
	// Chrominance DC.
	{
		[16]byte{0, 3, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0, 0},
		[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11},
	},
	// Chrominance AC.
	{
		[16]byte{0, 2, 1, 2, 4, 4, 3, 4, 7, 5, 4, 4, 0, 1, 2, 119},
		[]byte{
			0x00, 0x01, 0x02, 0x03, 0x11, 0x04, 0x05, 0x21,
			0x31, 0x06, 0x12, 0x41, 0x51, 0x07, 0x61, 0x71,
			0x13, 0x22, 0x32, 0x81, 0x08, 0x14, 0x42, 0x91,
			0xa1, 0xb1, 0xc1, 0x09, 0x23, 0x33, 0x52, 0xf0,
			0x15, 0x62, 0x72, 0xd1, 0x0a, 0x16, 0x24, 0x34,
			0xe1, 0x25, 0xf1, 0x17, 0x18, 0x19, 0x1a, 0x26,
			0x27, 0x28, 0x29, 0x2a, 0x35, 0x36, 0x37, 0x38,
			0x39, 0x3a, 0x43, 0x44, 0x45, 0x46, 0x47, 0x48,
			0x49, 0x4a, 0x53, 0x54, 0x55, 0x56, 0x57, 0x58,
			0x59, 0x5a, 0x63, 0x64, 0x65, 0x66, 0x67, 0x68,
			0x69, 0x6a, 0x73, 0x74, 0x75, 0x76, 0x77, 0x78,
			0x79, 0x7a, 0x82, 0x83, 0x84, 0x85, 0x86, 0x87,
			0x88, 0x89, 0x8a, 0x92, 0x93, 0x94, 0x95, 0x96,
			0x97, 0x98, 0x99, 0x9a, 0xa2, 0xa3, 0xa4, 0xa5,
			0xa6, 0xa7, 0xa8, 0xa9, 0xaa, 0xb2, 0xb3, 0xb4,
			0xb5, 0xb6, 0xb7, 0xb8, 0xb9, 0xba, 0xc2, 0xc3,
			0xc4, 0xc5, 0xc6, 0xc7, 0xc8, 0xc9, 0xca, 0xd2,
			0xd3, 0xd4, 0xd5, 0xd6, 0xd7, 0xd8, 0xd9, 0xda,
			0xe2, 0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8, 0xe9,
			0xea, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8,
			0xf9, 0xfa,
		},
	},
}

// huffmanLUT is a compiled look-up table representation of a huffmanSpec.
// Each value maps to a uint32 of which the 8 most significant bits hold the
// codeword size in bits and the 24 least significant bits hold the codeword.
// The maximum codeword size is 16 bits.
type huffmanLUT []uint32

func (h *huffmanLUT) init(s huffmanSpec) {
	maxValue := 0
	for _, v := range s.value {
		if int(v) > maxValue {
			maxValue = int(v)
		}
	}
	*h = make([]uint32, maxValue+1)
	code, k := uint32(0), 0
	for i := 0; i < len(s.count); i++ {
		nBits := uint32(i+1) << 24
		for j := uint8(0); j < s.count[i]; j++ {
			(*h)[s.value[k]] = nBits | code
			code++
			k++
		}
		code <<= 1
	}
}

// theHuffmanLUT are compiled representations of theHuffmanSpec.
var theHuffmanLUT [4]huffmanLUT

func init() {
	for i, s := range theHuffmanSpec {
		theHuffmanLUT[i].init(s)
	}
}

// writer is a buffered writer.
type writer interface {
	Flush() error
	io.Writer
	io.ByteWriter
}

// encoder encodes an image to the JPEG format.
type jpegEncoder struct {
	// w is the writer to write to. err is the first error encountered during
	// writing. All attempted writes after the first error become no-ops.
	w   writer
	err error
	// buf is a scratch buffer.
	buf [16]byte
	// bits and nBits are accumulated bits to write to w.
	bits, nBits uint32
	// quant is the scaled quantization tables, in zig-zag order.
	quant [nQuantIndex][jpegBlockSize]byte
}

func (e *jpegEncoder) flush() {
	if e.err != nil {
		return
	}
	e.err = e.w.Flush()
}

func (e *jpegEncoder) write(p []byte) {
	if e.err != nil {
		return
	}
	_, e.err = e.w.Write(p)
}

func (e *jpegEncoder) writeByte(b byte) {
	if e.err != nil {
		return
	}
	e.err = e.w.WriteByte(b)
}

// emit emits the least significant nBits bits of bits to the bit-stream.
// The precondition is bits < 1<<nBits && nBits <= 16.
func (e *jpegEncoder) emit(bits, nBits uint32) {
	nBits += e.nBits
	bits <<= 32 - nBits
	bits |= e.bits
	for nBits >= 8 {
		b := uint8(bits >> 24)
		e.writeByte(b)
		if b == 0xff {
			e.writeByte(0x00)
		}
		bits <<= 8
		nBits -= 8
	}
	e.bits, e.nBits = bits, nBits
}

// emitHuff emits the given value with the given Huffman encoder.
func (e *jpegEncoder) emitHuff(h huffIndex, value int32) {
	x := theHuffmanLUT[h][value]
	e.emit(x&(1<<24-1), x>>24)
}

// emitHuffRLE emits a run of runLength copies of value encoded with the given
// Huffman encoder.
func (e *jpegEncoder) emitHuffRLE(h huffIndex, runLength, value int32) {
	a, b := value, value
	if a < 0 {
		a, b = -value, value-1
	}
	var nBits uint32
	if a < 0x100 {
		nBits = uint32(jpegBitCount[a])
	} else {
		nBits = 8 + uint32(jpegBitCount[a>>8])
	}
	e.emitHuff(h, runLength<<4|int32(nBits))
	if nBits > 0 {
		e.emit(uint32(b)&(1<<nBits-1), nBits)
	}
}

// writeMarkerHeader writes the header for a marker with the given length.
func (e *jpegEncoder) writeMarkerHeader(marker uint8, markerlen int) {
	e.buf[0] = 0xff
	e.buf[1] = marker
	e.buf[2] = uint8(markerlen >> 8)
	e.buf[3] = uint8(markerlen & 0xff)
	e.write(e.buf[:4])
}

// writeDQT writes the Define Quantization Table marker.
func (e *jpegEncoder) writeDQT() {
	const markerlen = 2 + int(nQuantIndex)*(1+jpegBlockSize)
	e.writeMarkerHeader(jpegDQTMarker, markerlen)
	for i := range e.quant {
		e.writeByte(uint8(i))
		e.write(e.quant[i][:])
	}
}

// writeSOF0 writes the Start Of Frame (Baseline Sequential) marker.
func (e *jpegEncoder) writeSOF0(size image.Point, nComponent int) {
	markerlen := 8 + 3*nComponent
	e.writeMarkerHeader(jpegSOF0Marker, markerlen)
	e.buf[0] = 8 // 8-bit color.
	e.buf[1] = uint8(size.Y >> 8)
	e.buf[2] = uint8(size.Y & 0xff)
	e.buf[3] = uint8(size.X >> 8)
	e.buf[4] = uint8(size.X & 0xff)
	e.buf[5] = uint8(nComponent)
	if nComponent == 1 {
		e.buf[6] = 1
		// No subsampling for grayscale image.
		e.buf[7] = 0x11
		e.buf[8] = 0x00
	} else {
		for i := 0; i < nComponent; i++ {
			e.buf[3*i+6] = uint8(i + 1)
			// 4:4:4: every component sampled 1x1, matching image.YCbCrSubsampleRatio444.
			e.buf[3*i+7] = 0x11
			e.buf[3*i+8] = "\x00\x01\x01"[i]
		}
	}
	e.write(e.buf[:3*(nComponent-1)+9])
}

// writeDHT writes the Define Huffman Table marker.
func (e *jpegEncoder) writeDHT(nComponent int) {
	markerlen := 2
	specs := theHuffmanSpec[:]
	if nComponent == 1 {
		// Drop the Chrominance tables.
		specs = specs[:2]
	}
	for _, s := range specs {
		markerlen += 1 + 16 + len(s.value)
	}
	e.writeMarkerHeader(jpegDHTMarker, markerlen)
	for i, s := range specs {
		e.writeByte("\x00\x10\x01\x11"[i])
		e.write(s.count[:])
		e.write(s.value)
	}
}

// writeBlock writes a block of pixel data using the given quantization table,
// returning the post-quantized DC value of the DCT-transformed block. b is in
// natural (not zig-zag) order.
func (e *jpegEncoder) writeBlock(b *jpegBlock, q quantIndex, prevDC int32) int32 {
	fdct(b)
	// Emit the DC delta.
	dc := jpegDiv(b[0], 8*int32(e.quant[q][0]))
	e.emitHuffRLE(huffIndex(2*q+0), 0, dc-prevDC)
	// Emit the AC components.
	h, runLength := huffIndex(2*q+1), int32(0)
	for zig := 1; zig < jpegBlockSize; zig++ {
		ac := jpegDiv(b[unzig[zig]], 8*int32(e.quant[q][zig]))
		if ac == 0 {
			runLength++
		} else {
			for runLength > 15 {
				e.emitHuff(h, 0xf0)
				runLength -= 16
			}
			e.emitHuffRLE(h, runLength, ac)
			runLength = 0
		}
	}
	if runLength > 0 {
		e.emitHuff(h, 0x00)
	}
	return dc
}

// toYCbCr converts the 8x8 region of m whose top-left corner is p to its
// YCbCr values.
func toYCbCr(m image.Image, p image.Point, yBlock, cbBlock, crBlock *jpegBlock) {
	b := m.Bounds()
	xmax := b.Max.X - 1
	ymax := b.Max.Y - 1
	for j := 0; j < 8; j++ {
		for i := 0; i < 8; i++ {
			r, g, b, _ := m.At(min(p.X+i, xmax), min(p.Y+j, ymax)).RGBA()
			yy, cb, cr := color.RGBToYCbCr(uint8(r>>8), uint8(g>>8), uint8(b>>8))
			yBlock[8*j+i] = int32(yy)
			cbBlock[8*j+i] = int32(cb)
			crBlock[8*j+i] = int32(cr)
		}
	}
}

// grayToY stores the 8x8 region of m whose top-left corner is p in yBlock.
func grayToY(m *image.Gray, p image.Point, yBlock *jpegBlock) {
	b := m.Bounds()
	xmax := b.Max.X - 1
	ymax := b.Max.Y - 1
	pix := m.Pix
	for j := 0; j < 8; j++ {
		for i := 0; i < 8; i++ {
			idx := m.PixOffset(min(p.X+i, xmax), min(p.Y+j, ymax))
			yBlock[8*j+i] = int32(pix[idx])
		}
	}
}

// rgbaToYCbCr is a specialized version of toYCbCr for image.RGBA images.
func rgbaToYCbCr(m *image.RGBA, p image.Point, yBlock, cbBlock, crBlock *jpegBlock) {
	b := m.Bounds()
	xmax := b.Max.X - 1
	ymax := b.Max.Y - 1
	for j := 0; j < 8; j++ {
		sj := p.Y + j
		if sj > ymax {
			sj = ymax
		}
		offset := (sj-b.Min.Y)*m.Stride - b.Min.X*4
		for i := 0; i < 8; i++ {
			sx := p.X + i
			if sx > xmax {
				sx = xmax
			}
			pix := m.Pix[offset+sx*4:]
			yy, cb, cr := color.RGBToYCbCr(pix[0], pix[1], pix[2])
			yBlock[8*j+i] = int32(yy)
			cbBlock[8*j+i] = int32(cb)
			crBlock[8*j+i] = int32(cr)
		}
	}
}

// yCbCrToYCbCr is a specialized version of toYCbCr for image.YCbCr images.
func yCbCrToYCbCr(m *image.YCbCr, p image.Point, yBlock, cbBlock, crBlock *jpegBlock) {
	b := m.Bounds()
	xmax := b.Max.X - 1
	ymax := b.Max.Y - 1
	for j := 0; j < 8; j++ {
		sy := p.Y + j
		if sy > ymax {
			sy = ymax
		}
		for i := 0; i < 8; i++ {
			sx := p.X + i
			if sx > xmax {
				sx = xmax
			}
			yi := m.YOffset(sx, sy)
			ci := m.COffset(sx, sy)
			yBlock[8*j+i] = int32(m.Y[yi])
			cbBlock[8*j+i] = int32(m.Cb[ci])
			crBlock[8*j+i] = int32(m.Cr[ci])
		}
	}
}

// sosHeaderY is the SOS marker "\xff\xda" followed by 8 bytes:
//   - the marker length "\x00\x08",
//   - the number of components "\x01",
//   - component 1 uses DC table 0 and AC table 0 "\x01\x00",
//   - the bytes "\x00\x3f\x00". Section B.2.3 of the spec says that for
//     sequential DCTs, those bytes (8-bit Ss, 8-bit Se, 4-bit Ah, 4-bit Al)
//     should be 0x00, 0x3f, 0x00<<4 | 0x00.
var sosHeaderY = []byte{
	0xff, 0xda, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3f, 0x00,
}

// sosHeaderYCbCr is the SOS marker "\xff\xda" followed by 12 bytes:
//   - the marker length "\x00\x0c",
//   - the number of components "\x03",
//   - component 1 uses DC table 0 and AC table 0 "\x01\x00",
//   - component 2 uses DC table 1 and AC table 1 "\x02\x11",
//   - component 3 uses DC table 1 and AC table 1 "\x03\x11",
//   - the bytes "\x00\x3f\x00". Section B.2.3 of the spec says that for
//     sequential DCTs, those bytes (8-bit Ss, 8-bit Se, 4-bit Ah, 4-bit Al)
//     should be 0x00, 0x3f, 0x00<<4 | 0x00.
var sosHeaderYCbCr = []byte{
	0xff, 0xda, 0x00, 0x0c, 0x03, 0x01, 0x00, 0x02,
	0x11, 0x03, 0x11, 0x00, 0x3f, 0x00,
}

// writeSOS writes the StartOfScan marker.
func (e *jpegEncoder) writeSOS(m image.Image) {
	switch m.(type) {
	case *image.Gray:
		e.write(sosHeaderY)
	default:
		e.write(sosHeaderYCbCr)
	}
	var (
		// Scratch buffers to hold the YCbCr values.
		// The blocks are in natural (not zig-zag) order.
		b jpegBlock
		// DC components are delta-encoded.
		prevDCY, prevDCCb, prevDCCr int32
	)
	bounds := m.Bounds()
	switch m := m.(type) {
	// TODO(wathiede): switch on m.ColorModel() instead of type.
	case *image.Gray:
		for y := bounds.Min.Y; y < bounds.Max.Y; y += 8 {
			for x := bounds.Min.X; x < bounds.Max.X; x += 8 {
				p := image.Pt(x, y)
				grayToY(m, p, &b)
				prevDCY = e.writeBlock(&b, 0, prevDCY)
			}
		}
	default:
		var cbBlock, crBlock jpegBlock
		rgba, _ := m.(*image.RGBA)
		ycbcr, _ := m.(*image.YCbCr)
		for y := bounds.Min.Y; y < bounds.Max.Y; y += 8 {
			for x := bounds.Min.X; x < bounds.Max.X; x += 8 {
				p := image.Pt(x, y)
				if rgba != nil {
					rgbaToYCbCr(rgba, p, &b, &cbBlock, &crBlock)
				} else if ycbcr != nil {
					yCbCrToYCbCr(ycbcr, p, &b, &cbBlock, &crBlock)
				} else {
					toYCbCr(m, p, &b, &cbBlock, &crBlock)
				}
				prevDCY = e.writeBlock(&b, 0, prevDCY)
				prevDCCb = e.writeBlock(&cbBlock, 1, prevDCCb)
				prevDCCr = e.writeBlock(&crBlock, 1, prevDCCr)
			}
		}
	}
	// Pad the last byte with 1's.
	e.emit(0x7f, 7)
}

func encodeJPEG444(w io.Writer, m image.Image, quality int) error {
	b := m.Bounds()
	if b.Dx() >= 1<<16 || b.Dy() >= 1<<16 {
		return errJPEGTooLarge
	}
	if _, ok := m.(*image.Gray); !ok {
		m = imageToYCbCr444(m)
	}
	var e jpegEncoder
	if ww, ok := w.(writer); ok {
		e.w = ww
	} else {
		e.w = bufio.NewWriter(w)
	}
	if quality < 1 {
		quality = 1
	} else if quality > 100 {
		quality = 100
	}
	// Convert from a quality rating to a scaling factor.
	var scale int
	if quality < 50 {
		scale = 5000 / quality
	} else {
		scale = 200 - quality*2
	}
	// Initialize the quantization tables.
	for i := range e.quant {
		for j := range e.quant[i] {
			x := int(unscaledQuant[i][j])
			x = (x*scale + 50) / 100
			if x < 1 {
				x = 1
			} else if x > 255 {
				x = 255
			}
			e.quant[i][j] = uint8(x)
		}
	}
	// Compute number of components based on input image type.
	nComponent := 3
	switch m.(type) {
	// TODO(wathiede): switch on m.ColorModel() instead of type.
	case *image.Gray:
		nComponent = 1
	}
	// Write the Start Of Image marker.
	e.buf[0] = 0xff
	e.buf[1] = 0xd8
	e.write(e.buf[:2])
	// Write the quantization tables.
	e.writeDQT()
	// Write the image dimensions.
	e.writeSOF0(b.Size(), nComponent)
	// Write the Huffman tables.
	e.writeDHT(nComponent)
	// Write the image data.
	e.writeSOS(m)
	// Write the End Of Image marker.
	e.buf[0] = 0xff
	e.buf[1] = 0xd9
	e.write(e.buf[:2])
	e.flush()
	return e.err
}

func dctBox(x0, x1, kcos, ksin int32) (y0, y1 int32) {
	// y0 = x0*kcos + x1*ksin
	// y1 = -x0*ksin + x1*kcos
	ksum := kcos * (x0 + x1)
	y0 = ksum + (ksin-kcos)*x1
	y1 = ksum - (kcos+ksin)*x0
	return y0, y1
}

// Constants needed for the implementation.
// These are all 60-bit precision fixed-point constants.
// The function c(val, b) rounds the constant to b bits.
// c is simple enough that calls to it with constant args
// are inlined and constant-propagated down to an inline constant.
// Each constant is commented with its Ivy definition (see robpike.io/ivy),
// using this scaling helper function:
//
//	op fix x = floor 0.5 + x * 2**60
const (
	cos1       = 1130768441178740757 // fix cos 1*pi/16
	sin1       = 224923827593068887  // fix sin 1*pi/16
	cos3       = 958619196450722178  // fix cos 3*pi/16
	sin3       = 640528868967736374  // fix sin 3*pi/16
	sqrt2      = 1630477228166597777 // fix sqrt 2
	sqrt2_cos6 = 623956622067911264  // fix (sqrt 2)*cos 6*pi/16
	sqrt2_sin6 = 1506364539328854985 // fix (sqrt 2)*sin 6*pi/16
)

func c(x uint64, bits int) int32 {
	return int32((x + (1 << (59 - bits))) >> (60 - bits))
}

// fdct implements the forward DCT.
// Inputs are UQ8.0; outputs are Q13.0.
func fdct(b *jpegBlock) {
	fdctCols(b)
	fdctRows(b)
}

// fdctCols applies the 1D DCT to the columns of b.
// Inputs are UQ8.0 in [0,255] but interpreted as [-128,127].
// Outputs are Q10.18.
func fdctCols(b *jpegBlock) {
	for i := range 8 {
		x0 := b[0*8+i]
		x1 := b[1*8+i]
		x2 := b[2*8+i]
		x3 := b[3*8+i]
		x4 := b[4*8+i]
		x5 := b[5*8+i]
		x6 := b[6*8+i]
		x7 := b[7*8+i]

		// x[01234567] are UQ8.0 in [0,255].

		// Stage 1: four butterflies.
		// In general a butterfly of QN.M inputs produces Q(N+1).M outputs.
		// A butterfly of UQN.M inputs produces a UQ(N+1).M sum and a QN.M difference.

		x0, x7 = x0+x7, x0-x7
		x1, x6 = x1+x6, x1-x6
		x2, x5 = x2+x5, x2-x5
		x3, x4 = x3+x4, x3-x4
		// x[0123] now UQ9.0 in [0, 510].
		// x[4567] now Q8.0 in [-255,255].

		// Stage 2: two boxes and two butterflies.
		// A box on QN.M inputs with B-bit constants
		// produces Q(N+1).(M+B) outputs.
		// (The +1 is from the addition.)

		x4, x7 = dctBox(x4, x7, c(cos3, 18), c(sin3, 18))
		x5, x6 = dctBox(x5, x6, c(cos1, 18), c(sin1, 18))
		// x[47] now Q9.18 in [-354, 354].
		// x[56] now Q9.18 in [-300, 300].

		x0, x3 = x0+x3, x0-x3
		x1, x2 = x1+x2, x1-x2
		// x[01] now UQ10.0 in [0, 1020].
		// x[23] now Q9.0 in [-510, 510].

		// Stage 3: one box and three butterflies.

		x2, x3 = dctBox(x2, x3, c(sqrt2_cos6, 18), c(sqrt2_sin6, 18))
		// x[23] now Q10.18 in [-943, 943].

		x0, x1 = x0+x1, x0-x1
		// x0 now UQ11.0 in [0, 2040].
		// x1 now Q10.0 in [-1020, 1020].

		// Store x0, x1, x2, x3 to their permuted targets.
		// The original +128 in every input value
		// has cancelled out except in the “DC signal” x0.
		// Subtracting 128*8 here is equivalent to subtracting 128
		// from every input before we started, but cheaper.
		// It also converts x0 from UQ11.18 to Q10.18.
		b[0*8+i] = (x0 - 128*8) << 18
		b[4*8+i] = x1 << 18
		b[2*8+i] = x2
		b[6*8+i] = x3

		x4, x6 = x4+x6, x4-x6
		x7, x5 = x7+x5, x7-x5
		// x[4567] now Q10.18 in [-654, 654].

		// Stage 4: two √2 scalings and one butterfly.

		x5 = (x5 >> 12) * c(sqrt2, 12)
		x6 = (x6 >> 12) * c(sqrt2, 12)
		// x[56] still Q10.18 in [-925, 925] (= 654√2).
		x7, x4 = x7+x4, x7-x4
		// x[47] still Q10.18 in [-925, 925] (not Q11.18!).
		// This is not obvious at all! See “Note on 925” below.

		// Store x4 x5 x6 x7 to their permuted targets.
		b[1*8+i] = x7
		b[3*8+i] = x5
		b[5*8+i] = x6
		b[7*8+i] = x4
	}
}

// fdctRows applies the 1D DCT to the rows of b.
// Inputs are Q10.18; outputs are Q13.0.
func fdctRows(b *jpegBlock) {
	for i := range 8 {
		x := b[8*i : 8*i+8 : 8*i+8]
		x0 := x[0]
		x1 := x[1]
		x2 := x[2]
		x3 := x[3]
		x4 := x[4]
		x5 := x[5]
		x6 := x[6]
		x7 := x[7]

		// x[01234567] are Q10.18 [-1020, 1020].

		// Stage 1: four butterflies.

		x0, x7 = x0+x7, x0-x7
		x1, x6 = x1+x6, x1-x6
		x2, x5 = x2+x5, x2-x5
		x3, x4 = x3+x4, x3-x4
		// x[01234567] now Q11.18 in [-2040, 2040].

		// Stage 2: two boxes and two butterflies.

		x4, x7 = dctBox(x4>>14, x7>>14, c(cos3, 14), c(sin3, 14))
		x5, x6 = dctBox(x5>>14, x6>>14, c(cos1, 14), c(sin1, 14))
		// x[47] now Q12.18 in [-2830, 2830].
		// x[56] now Q12.18 in [-2400, 2400].
		x0, x3 = x0+x3, x0-x3
		x1, x2 = x1+x2, x1-x2
		// x[01234567] now Q12.18 in [-4080, 4080].

		// Stage 3: one box and three butterflies.

		x2, x3 = dctBox(x2>>14, x3>>14, c(sqrt2_cos6, 14), c(sqrt2_sin6, 14))
		// x[23] now Q13.18 in [-7539, 7539].
		x0, x1 = x0+x1, x0-x1
		// x[01] now Q13.18 in [-8160, 8160].
		x4, x6 = x4+x6, x4-x6
		x7, x5 = x7+x5, x7-x5
		// x[4567] now Q13.18 in [-5230, 5230].

		// Stage 4: two √2 scalings and one butterfly.

		x5 = (x5 >> 14) * c(sqrt2, 14)
		x6 = (x6 >> 14) * c(sqrt2, 14)
		// x[56] still Q13.18 in [-7397, 7397] (= 5230√2).
		x7, x4 = x7+x4, x7-x4
		// x[47] still Q13.18 in [-7395, 7395] (= 2040*3.6246).
		// See “Note on 925” below.

		// Cut from Q13.18 to Q13.0.
		x0 = (x0 + 1<<17) >> 18
		x1 = (x1 + 1<<17) >> 18
		x2 = (x2 + 1<<17) >> 18
		x3 = (x3 + 1<<17) >> 18
		x4 = (x4 + 1<<17) >> 18
		x5 = (x5 + 1<<17) >> 18
		x6 = (x6 + 1<<17) >> 18
		x7 = (x7 + 1<<17) >> 18

		// Note: Unlike in fdctCols, saved all stores for the end
		// because they are adjacent memory locations and some systems
		// can use multiword stores.
		x[0] = x0
		x[1] = x7
		x[2] = x2
		x[3] = x5
		x[4] = x1
		x[5] = x6
		x[6] = x3
		x[7] = x4
	}
}

// “Note on 925”, deferred from above to avoid interrupting code.
//
// In fdctCols, heading into stage 2, the values x4, x5, x6, x7 are in [-255, 255].
// Let's call those specific values b4, b5, b6, b7, and trace how x[4567] evolve:
//
// Stage 2:
//	x4 = b4*cos3 + b7*sin3
//	x7 = -b4*sin3 + b7*cos3
//	x5 = b5*cos1 + b6*sin1
//	x6 = -b5*sin1 + b6*cos1
//
// Stage 3:
//
//	x4 = x4+x6 =  b4*cos3 + b7*sin3 - b5*sin1 + b6*cos1
//	x6 = x4-x6 =  b4*cos3 + b7*sin3 + b5*sin1 - b6*cos1
//	x7 = x7+x5 = -b4*sin3 + b7*cos3 + b5*cos1 + b6*sin1
//	x5 = x7-x5 = -b4*sin3 + b7*cos3 - b5*cos1 - b6*sin1
//
// Stage 4:
//
//	x7 = x7+x4 = -b4*sin3 + b7*cos3 + b5*cos1 + b6*sin1 + b4*cos3 + b7*sin3 - b5*sin1 + b6*cos1
//	   = b4*(cos3-sin3) + b5*(cos1-sin1) + b6*(cos1+sin1) + b7*(cos3+sin3)
//	   < 255*(0.2759 + 0.7857 + 1.1759 + 1.3871) = 255*3.6246 < 925.
//
//	x4 = x7-x4 = -b4*sin3 + b7*cos3 + b5*cos1 + b6*sin1 - b4*cos3 - b7*sin3 + b5*sin1 - b6*cos1
//	   = -b4*(cos3+sin3) + b5*(cos1+sin1) + b6*(sin1-cos1) + b7*(cos3-sin3)
//	   < same 925.
//
// The fact that x5, x6 are also at most 925 is not a coincidence: we are computing
// the same kinds of numbers for all four, just with different paths to them.
//
// In fdctRows, the same analysis applies, but the initial values are
// in [-2040, 2040] instead of [-255, 255], so the bound is 2040*3.6246 < 7395.

var errJPEGTooLarge = errJPEG("jpeg: image is too large to encode")

type jpegStaticError string

func (e jpegStaticError) Error() string { return string(e) }

func errJPEG(s string) error { return jpegStaticError(s) }

func imageToYCbCr444(src image.Image) *image.YCbCr {
	b := src.Bounds()
	dst := image.NewYCbCr(b, image.YCbCrSubsampleRatio444)
	switch img := src.(type) {
	case *image.YCbCr:
		if img.SubsampleRatio == image.YCbCrSubsampleRatio444 {
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					yi := dst.YOffset(x, y)
					ci := dst.COffset(x, y)
					syi := img.YOffset(x, y)
					sci := img.COffset(x, y)
					dst.Y[yi] = img.Y[syi]
					dst.Cb[ci] = img.Cb[sci]
					dst.Cr[ci] = img.Cr[sci]
				}
			}
			return dst
		}
	case *image.RGBA:
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				i := img.PixOffset(x, y)
				yy, cb, cr := color.RGBToYCbCr(img.Pix[i], img.Pix[i+1], img.Pix[i+2])
				yi := dst.YOffset(x, y)
				ci := dst.COffset(x, y)
				dst.Y[yi] = yy
				dst.Cb[ci] = cb
				dst.Cr[ci] = cr
			}
		}
		return dst
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := src.At(x, y).RGBA()
			yy, cb, cr := color.RGBToYCbCr(uint8(r>>8), uint8(g>>8), uint8(bl>>8))
			yi := dst.YOffset(x, y)
			ci := dst.COffset(x, y)
			dst.Y[yi] = yy
			dst.Cb[ci] = cb
			dst.Cr[ci] = cr
		}
	}
	return dst
}
