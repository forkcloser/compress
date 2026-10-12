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

import "errors"

var (
	// ErrUnknownFormat is returned by Decompress when the stream's leading
	// bytes are no registered format's magic, or when no format is
	// registered at all.
	ErrUnknownFormat = errors.New("unrecognized compression format")

	// ErrPathTraversal indicates a tar entry attempted to escape the target directory.
	ErrPathTraversal = errors.New("tar entry outside target directory")
)
