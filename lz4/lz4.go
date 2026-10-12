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

package lz4

import (
	"fmt"
	"io"

	plz4 "github.com/pierrec/lz4/v4"

	"github.com/forkcloser/compress"
)

//nolint:gochecknoinits // Registration pattern requires init.
func init() {
	compress.Register("lz4", []byte{0x04, 0x22, 0x4D, 0x18}, New)
}

// New returns an lz4 decompressed reader that decodes frame blocks on
// GOMAXPROCS goroutines. Abandoning a partially consumed reader parks those
// workers and their block buffers for good, as the library has no
// cancellation: a consumer that stops short of EOF by design (a tar reader
// halting at the end-of-archive marker) must drain the rest before dropping
// the reader.
func New(reader io.Reader) (io.ReadCloser, error) {
	decoder := plz4.NewReader(reader)
	if err := decoder.Apply(plz4.ConcurrencyOption(-1)); err != nil {
		return nil, fmt.Errorf("lz4 concurrency: %w", err)
	}

	return io.NopCloser(decoder), nil
}
