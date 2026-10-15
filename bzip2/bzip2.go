/*
   Copyright Mycophonic.
   Copyright Forkcloser.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package bzip2

import (
	"io"

	"github.com/forkcloser/compress"
	"github.com/forkcloser/compress/internal/codec"
)

//nolint:gochecknoinits // Registration pattern requires init.
func init() {
	compress.Register("bzip2", []byte{0x42, 0x5A, 0x68}, codec.Bzip2)
}

// NewReader returns a reader that decompresses the bzip2 stream r holds. Its
// errors are compress's classes: ErrCorrupt for a stream the decoder refuses
// or cut short, ErrSource for a failure of r, ErrClosed for a Read after
// Close. There is no writer: Go has no bzip2 encoder.
func NewReader(r io.Reader) (io.ReadCloser, error) {
	return codec.Reader(codec.Bzip2, r)
}
