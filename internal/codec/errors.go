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

// Package codec is the module's codecs behind one shape: a raw constructor
// per format, and the wrappers that class every error a reader or writer
// returns against the source or destination underneath.
package codec

import "errors"

// The fault classes. Package compress exports them; every error a reader or
// writer here returns wraps one of them, with the cause in the chain.
var (
	// ErrUnknownFormat: a stream whose leading bytes are no registered
	// format's magic, or no format registered at all.
	ErrUnknownFormat = errors.New("unrecognized compression format")

	// ErrCorrupt: a stream that is not a valid encoding of its format, or cut
	// short, io.ErrUnexpectedEOF in the chain for a cut.
	ErrCorrupt = errors.New("corrupt stream")

	// ErrSource: the reader underneath a decoder failed, its own error in the
	// chain.
	ErrSource = errors.New("source failed")

	// ErrDestination: the writer underneath an encoder failed, its own error
	// in the chain.
	ErrDestination = errors.New("destination failed")

	// ErrClosed: a Read or Write after Close; os.ErrClosed or io.ErrClosedPipe
	// in the chain.
	ErrClosed = errors.New("closed")
)

// classed reports whether err already carries one of the classes.
func classed(err error) bool {
	return errors.Is(err, ErrUnknownFormat) || errors.Is(err, ErrCorrupt) || errors.Is(err, ErrSource) ||
		errors.Is(err, ErrDestination) || errors.Is(err, ErrClosed)
}
