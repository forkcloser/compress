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

package xz

import (
	"fmt"
	"io"

	uxz "github.com/forkcloser/xz"

	"github.com/forkcloser/compress"
)

//nolint:gochecknoinits // Registration pattern requires init.
func init() {
	compress.Register("xz", []byte{0xFD, 0x37, 0x7A, 0x58, 0x5A, 0x00}, New)
}

// New returns an xz decompressed reader, decoding ahead of its consumer.
func New(reader io.Reader) (io.ReadCloser, error) {
	xr, err := uxz.NewReader(reader)
	if err != nil {
		return nil, fmt.Errorf("xz decoder: %w", err)
	}

	return compress.ReadAhead(io.NopCloser(xr)), nil
}
