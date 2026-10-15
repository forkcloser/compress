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

package zstd

import (
	"io"

	"github.com/forkcloser/compress/internal/codec"
)

// NewReader returns a reader that decompresses the zstd stream r holds. Its
// errors are compress's classes: ErrCorrupt for a stream the decoder refuses
// or cut short, ErrSource for a failure of r, ErrClosed for a Read after
// Close.
func NewReader(r io.Reader) (io.ReadCloser, error) {
	return codec.Reader(codec.Zstd, r)
}

// NewWriter returns a writer that compresses to w as zstd; Close ends the
// stream and must be called. Its errors are compress's classes:
// ErrDestination for a failure of w, ErrClosed for a Write after Close.
func NewWriter(w io.Writer) (io.WriteCloser, error) {
	return codec.Writer(codec.ZstdWriter, w)
}
