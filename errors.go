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

import "github.com/forkcloser/compress/internal/codec"

// The fault classes. Every error this module returns, from this package and
// from the format packages' readers and writers, wraps one of them, with the
// specifics in the chain; a caller compares with errors.Is.
var (
	// ErrUnknownFormat is returned by Decompress when the stream's leading
	// bytes are no registered format's magic, or when no format is
	// registered at all. The input is not for this package.
	ErrUnknownFormat = codec.ErrUnknownFormat

	// ErrCorrupt is a stream that is not a valid encoding of its format: a
	// header or a block its decoder refuses, or a stream cut short, with
	// io.ErrUnexpectedEOF in the chain. The input is bad.
	ErrCorrupt = codec.ErrCorrupt

	// ErrSource is the reader a stream comes from failing, with its own error
	// in the chain. The caller that owns the reader acts on it.
	ErrSource = codec.ErrSource

	// ErrDestination is the writer an encoder writes to failing, with its own
	// error in the chain. The caller that owns the writer acts on it.
	ErrDestination = codec.ErrDestination

	// ErrClosed is a Read or Write after Close, or a compressed reader whose
	// consumer closed it before the encoder was done; os.ErrClosed or
	// io.ErrClosedPipe is in the chain. The calls came in the wrong order.
	ErrClosed = codec.ErrClosed
)
