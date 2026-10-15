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

package compress

import (
	"io"

	"github.com/forkcloser/compress/internal/codec"
)

// ReadAhead returns a reader that decodes from decoder on a goroutine of its
// own, a few megabytes ahead of the caller, so a consumer that is slower than
// the decoder (a parser) no longer runs in lockstep with it. It is for a
// decoder that is itself serial; one that already decodes concurrently gains
// nothing but a copy. The bytes, their order and the decoder's errors are
// exactly the decoder's: they are the caller's decoder's to class. Close
// stops the goroutine, once the decoder's current Read returns, and closes
// the decoder; a Read after Close, or parked when Close comes, is ErrClosed.
// A reader that is never closed keeps its goroutine.
func ReadAhead(decoder io.ReadCloser) io.ReadCloser {
	return codec.ReadAhead(decoder)
}
