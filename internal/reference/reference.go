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

// Package reference finds the tool a command's interop test compares with:
// the program of the same name on PATH that is not this module's own.
package reference

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ours is what every command of this module prints in its version banner.
const ours = "(forkcloser/compress)"

// Find returns the first program named name on PATH that runs with
// versionFlag and does not identify as this module's own, or skips the test
// naming what it found instead. Once this module's commands are on the PATH
// ahead of the system's (a tool manager puts them there), a plain lookup
// would hand the test its own binary and the comparison would prove nothing;
// a proxy with nothing behind it answers every call with an error and is no
// reference either.
func Find(tb testing.TB, name, versionFlag string) string {
	tb.Helper()

	var seen []string

	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}

		for _, candidate := range candidates(filepath.Join(dir, name)) {
			if info, err := os.Stat(candidate); err != nil || info.IsDir() {
				continue
			}

			if note := probe(tb, candidate, versionFlag); note != "" {
				seen = append(seen, candidate+" ("+note+")")

				continue
			}

			return candidate
		}
	}

	if len(seen) == 0 {
		tb.Skipf("no %s on PATH", name)
	}

	tb.Skipf("no %s on PATH to compare with: %s", name, strings.Join(seen, ", "))

	return ""
}

// probe runs the candidate's version banner and says why it is no reference,
// or nothing when it is one.
func probe(tb testing.TB, candidate, versionFlag string) string {
	tb.Helper()

	// #nosec G204 -- the candidate is a PATH entry the test probes
	out, err := exec.CommandContext(tb.Context(), candidate, versionFlag).CombinedOutput()
	if err != nil {
		return "does not run: " + err.Error()
	}

	if strings.Contains(string(out), ours) {
		return "this module's own"
	}

	return ""
}

// candidates are the file names a command may have on this platform.
func candidates(path string) []string {
	if runtime.GOOS != "windows" {
		return []string{path}
	}

	return []string{path + ".exe", path}
}
