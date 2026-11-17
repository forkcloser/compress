// Package tarblocks lays tar archives out block by block, for the tests of
// what archive/tar reads and does not write: sparse members.
package tarblocks

import (
	"bytes"
	"fmt"
	"strconv"
)

const (
	blockSize  = 512
	ustarMagic = "ustar\x0000"
	gnuMagic   = "ustar  \x00"
)

// SparseForms are the forms a sparse member is stored in: the three PAX
// ones and the old GNU one.
func SparseForms() []string {
	return []string{"PAX 1.0", "PAX 0.1", "PAX 0.0", "old GNU"}
}

// Sparse is an archive of two members: "hole", a sparse member in the form
// named, which states realSize bytes and carries none of them, and "after",
// a regular file holding "after". It is a few blocks whatever realSize is.
func Sparse(form string, realSize int64) []byte {
	after := append(header("after", '0', len("after")), padded("after")...)
	trailer := make([]byte, 2*blockSize)
	size := strconv.FormatInt(realSize, 10)

	// The two PAX forms before 1.0 carry the map in the records: as one
	// record, or as an offset and a length for each piece of data.
	sized := record("GNU.sparse.size", size) + record("GNU.sparse.numblocks", "1")

	inRecords := func(records string) []byte {
		return bytes.Join([][]byte{
			header("PaxHeaders.0/hole", 'x', len(records)), padded(records),
			header("hole", '0', 0),
			after, trailer,
		}, nil)
	}

	switch form {
	case "PAX 1.0":
		// The map is the member's first block of data: no piece.
		records := record("GNU.sparse.major", "1") + record("GNU.sparse.minor", "0") +
			record("GNU.sparse.name", "hole") + record("GNU.sparse.realsize", size)

		return bytes.Join([][]byte{
			header("PaxHeaders.0/hole", 'x', len(records)), padded(records),
			header("GNUSparseFile.0/hole", '0', blockSize), padded("0\n"),
			after, trailer,
		}, nil)
	case "PAX 0.1":
		return inRecords(sized + record("GNU.sparse.map", "0,0"))
	case "PAX 0.0":
		return inRecords(sized + record("GNU.sparse.offset", "0") + record("GNU.sparse.numbytes", "0"))
	case "old GNU":
		// The real size is in the header, at 483, after four empty map
		// entries.
		block := unsummed("hole", 'S', 0, gnuMagic)
		copy(block[483:495], fmt.Sprintf("%011o\x00", realSize))

		return bytes.Join([][]byte{summed(block), after, trailer}, nil)
	}

	panic("tarblocks: no sparse form " + form)
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
func padded(data string) []byte {
	return append([]byte(data), make([]byte, (blockSize-len(data)%blockSize)%blockSize)...)
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
