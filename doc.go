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

// Package compress provides streaming zstd compression, registry-based
// decompression, and tar archiving.
//
// Zstd decompression is built in. Importing a format package registers its
// decoder with Decompress, and each is a codec in its own right, a NewReader
// and a NewWriter (bzip2 has no writer: Go has none) for a consumer that
// names its format, as one building an archive does:
//
//	import "github.com/forkcloser/compress/gzip"
//	import "github.com/forkcloser/compress/xz"
//	import "github.com/forkcloser/compress/lz4"
//	import "github.com/forkcloser/compress/zstd"
//	import "github.com/forkcloser/compress/bzip2"
//
// Every error the module returns wraps one of five classes, compared with
// errors.Is: ErrUnknownFormat, ErrCorrupt, ErrSource, ErrDestination and
// ErrClosed; the cause is in the chain.
//
// The module also ships the tar, xz, gzip, zstd, zip and unzip commands under
// cmd/, pure-Go stand-ins for the tools of the same names.
package compress
