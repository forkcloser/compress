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
// Zstd compression and decompression are built in. Additional decompression
// formats are available via side-effect imports:
//
//	import _ "github.com/forkcloser/compress/gzip"
//	import _ "github.com/forkcloser/compress/xz"
//	import _ "github.com/forkcloser/compress/bzip2"
//	import _ "github.com/forkcloser/compress/lz4"
//
// The module also ships the tar, xz and gzip commands under cmd/, pure-Go
// stand-ins for the tools of the same names.
package compress
