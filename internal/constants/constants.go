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

// Package constants holds what the module's packages and tests share by
// value: the byte sizes they count in, and the modes the commands create
// files and directories with.
package constants

import "os"

// Byte sizes, untyped so they combine with any integer.
const (
	KiB = 1024
	MiB = 1024 * KiB
)

// The modes the commands create with, before the umask; an archive's own
// mode, where one is kept, is applied afterwards.
const (
	// FilePermissions is a regular file's mode.
	FilePermissions os.FileMode = 0o644
	// DirPermissions is a directory's mode.
	DirPermissions os.FileMode = 0o755
	// OwnerAccess is what a directory being filled needs whatever the archive
	// says of it: the owner's full access, nobody else's.
	OwnerAccess os.FileMode = 0o700
)
