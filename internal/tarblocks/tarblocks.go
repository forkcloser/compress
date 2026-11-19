// Package tarblocks lays tar archives out block by block, for the tests of
// what archive/tar reads and does not write: sparse members. It also says
// what such a member takes on disk once extracted.
package tarblocks

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

const (
	blockSize  = 512
	ustarMagic = "ustar\x0000"
	gnuMagic   = "ustar  \x00"
	// An old GNU header maps oldGNUPieces pieces of data without a block
	// of extension, from oldGNUMap on: twelve bytes for an offset, twelve
	// for a length. The real size is at oldGNURealSize.
	oldGNUPieces   = 4
	oldGNUMap      = 386
	oldGNUNumber   = 12
	oldGNURealSize = 483
)

// Piece is a stretch of a sparse member that the archive carries: the rest
// of the member is holes.
type Piece struct {
	Offset int64
	Data   []byte
}

// SparseForms are the forms a sparse member is stored in: the three PAX
// ones and the old GNU one.
func SparseForms() []string {
	return []string{"PAX 1.0", "PAX 0.1", "PAX 0.0", "old GNU"}
}

// Sparse is an archive of two members: "hole", a sparse member in the form
// named, which states realSize bytes and carries the pieces given, in
// order, and "after", a regular file holding "after". A member that ends
// in a hole has the empty piece at its end that GNU tar writes there.
func Sparse(form string, realSize int64, pieces ...Piece) []byte {
	after := append(header("after", '0', len("after")), padded([]byte("after"))...)
	trailer := make([]byte, 2*blockSize)
	size := strconv.FormatInt(realSize, 10)

	// Where the last piece ends: before the member does, when it ends in a
	// hole.
	var end int64
	for _, piece := range pieces {
		end = piece.Offset + int64(len(piece.Data))
	}

	if end < realSize {
		pieces = append(slices.Clip(pieces), Piece{Offset: realSize})
	}

	var data []byte
	for _, piece := range pieces {
		data = append(data, piece.Data...)
	}

	// The two PAX forms before 1.0 carry the map in the records: as one
	// record, or as an offset and a length for each piece.
	sized := record("GNU.sparse.size", size) + record("GNU.sparse.numblocks", strconv.Itoa(len(pieces)))

	inRecords := func(records string) []byte {
		return bytes.Join([][]byte{
			header("PaxHeaders.0/hole", 'x', len(records)), padded([]byte(records)),
			header("hole", '0', len(data)), padded(data),
			after, trailer,
		}, nil)
	}

	switch form {
	case "PAX 1.0":
		// The map is the member's first blocks of data: the count of
		// pieces, then an offset and a length for each, a line apiece.
		records := record("GNU.sparse.major", "1") + record("GNU.sparse.minor", "0") +
			record("GNU.sparse.name", "hole") + record("GNU.sparse.realsize", size)

		var lines strings.Builder

		fmt.Fprintf(&lines, "%d\n", len(pieces))

		for _, piece := range pieces {
			fmt.Fprintf(&lines, "%d\n%d\n", piece.Offset, len(piece.Data))
		}

		mapped := padded([]byte(lines.String()))

		return bytes.Join([][]byte{
			header("PaxHeaders.0/hole", 'x', len(records)), padded([]byte(records)),
			header("GNUSparseFile.0/hole", '0', len(mapped)+len(data)), mapped, padded(data),
			after, trailer,
		}, nil)
	case "PAX 0.1":
		entries := make([]string, 0, 2*len(pieces))
		for _, piece := range pieces {
			entries = append(entries, strconv.FormatInt(piece.Offset, 10), strconv.Itoa(len(piece.Data)))
		}

		return inRecords(sized + record("GNU.sparse.map", strings.Join(entries, ",")))
	case "PAX 0.0":
		var records strings.Builder

		records.WriteString(sized)

		for _, piece := range pieces {
			records.WriteString(record("GNU.sparse.offset", strconv.FormatInt(piece.Offset, 10)))
			records.WriteString(record("GNU.sparse.numbytes", strconv.Itoa(len(piece.Data))))
		}

		return inRecords(records.String())
	case "old GNU":
		if len(pieces) > oldGNUPieces {
			panic("tarblocks: an old GNU header maps four pieces")
		}

		block := unsummed("hole", 'S', len(data), gnuMagic)

		for i, piece := range pieces {
			entry := block[oldGNUMap+2*oldGNUNumber*i:]
			copy(entry[:oldGNUNumber], fmt.Sprintf("%011o\x00", piece.Offset))
			copy(entry[oldGNUNumber:2*oldGNUNumber], fmt.Sprintf("%011o\x00", len(piece.Data)))
		}

		copy(block[oldGNURealSize:oldGNURealSize+oldGNUNumber], fmt.Sprintf("%011o\x00", realSize))

		return bytes.Join([][]byte{summed(block), padded(data), after, trailer}, nil)
	}

	panic("tarblocks: no sparse form " + form)
}

// Holed is the sparse member these tests extract: 64 MiB stated, of which
// an archive carries a piece at the start, with two blocks of zeros inside
// it, and a piece of no round length past the middle. The rest is holes,
// the end included.
func Holed() (int64, []Piece) {
	const (
		realSize = 64 << 20
		block    = 4096
		// Lengths that are no whole number of blocks.
		more = 904
		end  = 777
	)

	first := bytes.Repeat([]byte("data"), block/len("data"))
	first = append(first, make([]byte, 2*block)...)
	first = append(first, bytes.Repeat([]byte("more"), more/len("more"))...)

	return realSize, []Piece{
		{Offset: 0, Data: first},
		{Offset: realSize/2 + blockSize, Data: bytes.Repeat([]byte("end"), end/len("end"))},
	}
}

var (
	errLength  = errors.New("not the length stated")
	errContent = errors.New("not the content stated")
)

// Check reads file to its end and reports whether it is realSize bytes
// long, the pieces where they are stated and zeros everywhere else.
func Check(file io.Reader, realSize int64, pieces ...Piece) error {
	const chunk = 1 << 20

	got := make([]byte, chunk)
	want := make([]byte, chunk)

	for offset := int64(0); ; {
		n, err := io.ReadFull(file, got)

		clear(want[:n])

		for _, piece := range pieces {
			// The part of the piece that falls in what was read.
			from := max(piece.Offset, offset)
			to := min(piece.Offset+int64(len(piece.Data)), offset+int64(n))

			if from < to {
				copy(want[from-offset:to-offset], piece.Data[from-piece.Offset:to-piece.Offset])
			}
		}

		if !bytes.Equal(got[:n], want[:n]) {
			return fmt.Errorf("%w: between %d and %d", errContent, offset, offset+int64(n))
		}

		offset += int64(n)

		switch {
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			if offset != realSize {
				return fmt.Errorf("%w: %d bytes, not %d", errLength, offset, realSize)
			}

			return nil
		case err != nil:
			return fmt.Errorf("read: %w", err)
		}
	}
}

// header is one ustar header block, with its checksum.
func header(name string, typeflag byte, size int) []byte {
	return summed(unsummed(name, typeflag, size, ustarMagic))
}

// unsummed is a header block before its checksum is known; magic is the
// eight bytes of magic and version.
func unsummed(name string, typeflag byte, size int, magic string) []byte {
	block := make([]byte, blockSize)
	copy(block[0:100], name)
	copy(block[100:108], "0000644\x00")
	copy(block[108:116], "0000000\x00")
	copy(block[116:124], "0000000\x00")
	copy(block[124:136], fmt.Sprintf("%011o\x00", size))
	copy(block[136:148], "00000000000\x00")
	block[156] = typeflag
	copy(block[257:265], magic)

	return block
}

// summed is block with its checksum field filled.
func summed(block []byte) []byte {
	copy(block[148:156], "        ")

	sum := 0
	for _, b := range block {
		sum += int(b)
	}

	copy(block[148:156], fmt.Sprintf("%06o\x00 ", sum))

	return block
}

// padded is data to a whole number of blocks.
func padded(data []byte) []byte {
	return append(slices.Clip(data), make([]byte, (blockSize-len(data)%blockSize)%blockSize)...)
}

// record is one extended header record, its own length first.
func record(key, value string) string {
	body := " " + key + "=" + value + "\n"
	size := len(body) + 1

	for len(strconv.Itoa(size))+len(body) != size {
		size = len(strconv.Itoa(size)) + len(body)
	}

	return strconv.Itoa(size) + body
}
