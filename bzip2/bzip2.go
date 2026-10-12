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
	gobzip2 "compress/bzip2"
	"io"

	"github.com/forkcloser/compress"
)

//nolint:gochecknoinits // Registration pattern requires init.
func init() {
	compress.Register("bzip2", []byte{0x42, 0x5A, 0x68}, New)
}

// New returns a bzip2 decompressed reader, decoding ahead of its consumer.
func New(reader io.Reader) (io.ReadCloser, error) {
	return compress.ReadAhead(io.NopCloser(gobzip2.NewReader(reader))), nil
}
