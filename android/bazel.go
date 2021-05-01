// Copyright 2021 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package android

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	"github.com/google/blueprint"
	"github.com/google/blueprint/proptools"
)

type bazelModuleProperties struct {
	// The label of the Bazel target replacing this Soong module. When run in conversion mode, this
	// will import the handcrafted build target into the autogenerated file. Note: this may result in
	// a conflict due to duplicate targets if bp2build_available is also set.
	Label *string

	// If true, bp2build will generate the converted Bazel target for this module. Note: this may
	// cause a conflict due to the duplicate targets if label is also set.
	//
	// This is a bool pointer to support tristates: true, false, not set.
	//
	// To opt-in a module, set bazel_module: { bp2build_available: true }
	// To opt-out a module, set bazel_module: { bp2build_available: false }
	// To defer the default setting for the directory, do not set the value.
	Bp2build_available *bool
}

// Properties contains common module properties for Bazel migration purposes.
type properties struct {
	// In USE_BAZEL_ANALYSIS=1 mode, this represents the Bazel target replacing
	// this Soong module.
	Bazel_module bazelModuleProperties
}

// BazelModuleBase contains the property structs with metadata for modules which can be converted to
// Bazel.
type BazelModuleBase struct {
	bazelProperties properties
}

// Bazelable is specifies the interface for modules that can be converted to Bazel.
type Bazelable interface {
	bazelProps() *properties
	HasHandcraftedLabel() bool
	HandcraftedLabel() string
	GetBazelLabel(ctx BazelConversionPathContext, module blueprint.Module) string
	ConvertWithBp2build(ctx BazelConversionPathContext) bool
	GetBazelBuildFileContents(c Config, path, name string) (string, error)
	ConvertedToBazel(ctx BazelConversionPathContext) bool
}

// BazelModule is a lightweight wrapper interface around Module for Bazel-convertible modules.
type BazelModule interface {
	Module
	Bazelable
}

// InitBazelModule is a wrapper function that decorates a BazelModule with Bazel-conversion
// properties.
func InitBazelModule(module BazelModule) {
	module.AddProperties(module.bazelProps())
}

// bazelProps returns the Bazel properties for the given BazelModuleBase.
func (b *BazelModuleBase) bazelProps() *properties {
	return &b.bazelProperties
}

// HasHandcraftedLabel returns whether this module has a handcrafted Bazel label.
func (b *BazelModuleBase) HasHandcraftedLabel() bool {
	return b.bazelProperties.Bazel_module.Label != nil
}

// HandcraftedLabel returns the handcrafted label for this module, or empty string if there is none
func (b *BazelModuleBase) HandcraftedLabel() string {
	return proptools.String(b.bazelProperties.Bazel_module.Label)
}

// GetBazelLabel returns the Bazel label for the given BazelModuleBase.
func (b *BazelModuleBase) GetBazelLabel(ctx BazelConversionPathContext, module blueprint.Module) string {
	if b.HasHandcraftedLabel() {
		return b.HandcraftedLabel()
	}
	if b.ConvertWithBp2build(ctx) {
		return bp2buildModuleLabel(ctx, module)
	}
	return "" // no label for unconverted module
}

// Configuration to decide if modules in a directory should default to true/false for bp2build_available
type Bp2BuildConfig map[string]BazelConversionConfigEntry
type BazelConversionConfigEntry int

const (
	// A sentinel value to be used as a key in Bp2BuildConfig for modules with
	// no package path. This is also the module dir for top level Android.bp
	// modules.
	BP2BUILD_TOPLEVEL = "."

	// iota + 1 ensures that the int value is not 0 when used in the Bp2buildAllowlist map,
	// which can also mean that the key doesn't exist in a lookup.

	// all modules in this package and subpackages default to bp2build_available: true.
	// allows modules to opt-out.
	Bp2BuildDefaultTrueRecursively BazelConversionConfigEntry = iota + 1

	// all modules in this package (not recursively) default to bp2build_available: false.
	// allows modules to opt-in.
	Bp2BuildDefaultFalse
)

var (
	// Do not write BUILD files for these directories
	// NOTE: this is not recursive
	bp2buildDoNotWriteBuildFileList = []string{
		// Don't generate these BUILD files - because external BUILD files already exist
		"external/boringssl",
		"external/brotli",
		"external/dagger2",
		"external/flatbuffers",
		"external/gflags",
		"external/google-fruit",
		"external/grpc-grpc",
		"external/grpc-grpc/test/core/util",
		"external/grpc-grpc/test/cpp/common",
		"external/grpc-grpc/third_party/address_sorting",
		"external/nanopb-c",
		"external/nos/host/generic",
		"external/nos/host/generic/libnos",
		"external/nos/host/generic/libnos/generator",
		"external/nos/host/generic/libnos_datagram",
		"external/nos/host/generic/libnos_transport",
		"external/nos/host/generic/nugget/proto",
		"external/perfetto",
		"external/protobuf",
		"external/rust/cxx",
		"external/rust/cxx/demo",
		"external/ruy",
		"external/tensorflow",
		"external/tensorflow/tensorflow/lite",
		"external/tensorflow/tensorflow/lite/java",
		"external/tensorflow/tensorflow/lite/kernels",
		"external/tflite-support",
		"external/tinyalsa_new",
		"external/wycheproof",
		"external/libyuv",
	}

	// Configure modules in these directories to enable bp2build_available: true or false by default.
	bp2buildDefaultConfig = Bp2BuildConfig{
		"bionic":                Bp2BuildDefaultTrueRecursively,
		"external/gwp_asan":     Bp2BuildDefaultTrueRecursively,
		"system/core/libcutils": Bp2BuildDefaultTrueRecursively,
		"system/core/property_service/libpropertyinfoparser": Bp2BuildDefaultTrueRecursively,
		"system/libbase":                  Bp2BuildDefaultTrueRecursively,
		"system/logging/liblog":           Bp2BuildDefaultTrueRecursively,
		"external/jemalloc_new":           Bp2BuildDefaultTrueRecursively,
		"external/fmtlib":                 Bp2BuildDefaultTrueRecursively,
		"external/arm-optimized-routines": Bp2BuildDefaultTrueRecursively,
	}

	// Per-module denylist to always opt modules out of both bp2build and mixed builds.
	bp2buildModuleDoNotConvertList = []string{
		// Things that transitively depend on unconverted libc_* modules.
		"libc_nopthread", // http://b/186821550, cc_library_static, depends on //bionic/libc:libc_bionic_ndk (http://b/186822256)
		//                                                     also depends on //bionic/libc:libc_tzcode (http://b/186822591)
		//                                                     also depends on //bionic/libc:libstdc++ (http://b/186822597)
		"libc_common",        // http://b/186821517, cc_library_static, depends on //bionic/libc:libc_nopthread (http://b/186821550)
		"libc_common_static", // http://b/186824119, cc_library_static, depends on //bionic/libc:libc_common (http://b/186821517)
		"libc_common_shared", // http://b/186824118, cc_library_static, depends on //bionic/libc:libc_common (http://b/186821517)
		"libc_nomalloc",      // http://b/186825031, cc_library_static, depends on //bionic/libc:libc_common (http://b/186821517)

		"libbionic_spawn_benchmark", // http://b/186824595, cc_library_static, depends on //external/google-benchmark (http://b/186822740)
		//                                                                also depends on //system/logging/liblog:liblog (http://b/186822772)

		"libc_malloc_debug",           // http://b/186824339, cc_library_static, depends on //system/libbase:libbase (http://b/186823646)
		"libc_malloc_debug_backtrace", // http://b/186824112, cc_library_static, depends on //external/libcxxabi:libc++demangle (http://b/186823773)

		"libcutils",         // http://b/186827426, cc_library, depends on //system/core/libprocessgroup:libprocessgroup_headers (http://b/186826841)
		"libcutils_sockets", // http://b/186826853, cc_library, depends on //system/libbase:libbase (http://b/186826479)

		"liblinker_debuggerd_stub", // http://b/186824327, cc_library_static, depends on //external/zlib:libz (http://b/186823782)
		//                                                               also depends on //system/libziparchive:libziparchive (http://b/186823656)
		//                                                               also depends on //system/logging/liblog:liblog (http://b/186822772)
		"liblinker_main", // http://b/186825989, cc_library_static, depends on //external/zlib:libz (http://b/186823782)
		//                                                     also depends on //system/libziparchive:libziparchive (http://b/186823656)
		//                                                     also depends on//system/logging/liblog:liblog (http://b/186822772)
		"liblinker_malloc", // http://b/186826466, cc_library_static, depends on //external/zlib:libz (http://b/186823782)
		//                                                       also depends on //system/libziparchive:libziparchive (http://b/186823656)
		//                                                       also depends on //system/logging/liblog:liblog (http://b/186822772)
		"libc_jemalloc_wrapper", // cc_library_static, depends on //external/jemalloc_new:libjemalloc5
		"libc_ndk",              // cc_library_static, depends on libc_bionic_ndk, libc_jemalloc_wrapper, libc_tzcode, libstdc++
		// libc: http://b/183064430
		// cc_library, depends on libc_jemalloc_wrapper (and possibly many others)
		// Also http://b/186816506: Handle static and shared props
		// Also http://b/186650430: version_script prop support
		// Also http://b/186651708: pack_relocations prop support
		// Also http://b/186576099: multilib props support
		"libc",

		// Compilation or linker error from command line and toolchain inconsistencies.
		// http://b/186388670: Make Bazel/Ninja command lines more similar.
		// http://b/186628704: Incorporate Soong's Clang flags into Bazel's toolchains.
		//
		"libc_tzcode",  // http://b/186822591: cc_library_static, error: expected expression
		"libjemalloc5", // http://b/186828626: cc_library, ld.lld: error: undefined symbol: memset, __stack_chk_fail, pthread_mutex_trylock..
		// libc_bionic_ndk, cc_library_static
		// Error: ISO C++ requires field designators...
		// Also http://b/186576099: multilib props support
		// Also http://b/183595873: product_variables support
		"libc_bionic_ndk",
		// libc_malloc_hooks, cc_library
		// Error: undefined symbol: __malloc_hook, __realloc_hook, __free_hook, __memalign_hook, memset, __errno
		// These symbols are defined in https://cs.android.com/android/platform/superproject/+/master:bionic/libc/bionic/malloc_common.cpp;l=57-60;drc=9cad8424ff7b0fa63b53cb9919eae31539b8561a
		// Also http://b/186650430: version_script prop support
		"libc_malloc_hooks",
		// http://b/186822597, libstdc++, cc_library
		// Error: undefined symbol: __errno, syscall, async_safe_fatal_no_abort, abort, malloc, free
		// Also http://b/186024507: depends on libc through system_shared_libraries.
		// Also http://b/186650430: version_script prop support
		// Also http://b/186651708: pack_relocations prop support
		"libstdc++",
		// http://b/183064661, libm:
		// cc_library, error: "expected register here" (and many others)
		// Also http://b/186024507: depends on libc through system_shared_libraries.
		// Also http://b/186650430: version_script prop support
		// Also http://b/186651708: pack_relocations prop support
		// Also http://b/186576099: multilib props support
		"libm",

		// http://b/186823769: Needs C++ STL support, includes from unconverted standard libraries in //external/libcxx
		// c++_static
		"fmtlib_ndk",  // cc_library, from c++_static
		"libbase_ndk", // http://b/186826477, cc_library, depends on fmtlib_ndk, which depends on c++_static
		// libcxx
		"libBionicBenchmarksUtils", // cc_library_static, fatal error: 'map' file not found, from libcxx
		"fmtlib",                   // cc_library_static, fatal error: 'cassert' file not found, from libcxx
		"libbase",                  // http://b/186826479, cc_library, fatal error: 'memory' file not found, from libcxx

		// http://b/186024507: Includes errors because of the system_shared_libs default value.
		// Missing -isystem bionic/libc/include through the libc/libm/libdl
		// default dependencies if system_shared_libs is unset.
		"liblog",                 // http://b/186822772: cc_library, 'sys/cdefs.h' file not found
		"libjemalloc5_jet",       // cc_library, 'sys/cdefs.h' file not found
		"libseccomp_policy",      // http://b/186476753: cc_library, 'linux/filter.h' not found
		"note_memtag_heap_async", // http://b/185127353: cc_library_static, error: feature.h not found
		"note_memtag_heap_sync",  // http://b/185127353: cc_library_static, error: feature.h not found

		// Tests. Handle later.
		"libbionic_tests_headers_posix", // http://b/186024507, cc_library_static, sched.h, time.h not found
		"libjemalloc5_integrationtest",
		"libjemalloc5_stresstestlib",
		"libjemalloc5_unittest",
	}

	// Per-module denylist to opt modules out of mixed builds. Such modules will
	// still be generated via bp2build.
	mixedBuildsDisabledList = []string{
		"libc_netbsd",                      // lberki@, cc_library_static, version script assignment of 'LIBC_PRIVATE' to symbol 'SHA1Final' failed: symbol not defined
		"libc_openbsd",                     // ruperts@, cc_library_static, OK for bp2build but error: duplicate symbol: strcpy for mixed builds
		"libsystemproperties",              // cparsons@, cc_library_static, wrong include paths
		"libpropertyinfoparser",            // cparsons@, cc_library_static, wrong include paths
		"libarm-optimized-routines-string", // jingwen@, cc_library_static, OK for bp2build but b/186615213 (asflags not handled in  bp2build), version script assignment of 'LIBC' to symbol 'memcmp' failed: symbol not defined (also for memrchr, strnlen)
	}

	// Used for quicker lookups
	bp2buildDoNotWriteBuildFile = map[string]bool{}
	bp2buildModuleDoNotConvert  = map[string]bool{}
	mixedBuildsDisabled         = map[string]bool{}
)

func init() {
	for _, moduleName := range bp2buildDoNotWriteBuildFileList {
		bp2buildDoNotWriteBuildFile[moduleName] = true
	}

	for _, moduleName := range bp2buildModuleDoNotConvertList {
		bp2buildModuleDoNotConvert[moduleName] = true
	}

	for _, moduleName := range mixedBuildsDisabledList {
		mixedBuildsDisabled[moduleName] = true
	}
}

func ShouldWriteBuildFileForDir(dir string) bool {
	if _, ok := bp2buildDoNotWriteBuildFile[dir]; ok {
		return false
	} else {
		return true
	}
}

// MixedBuildsEnabled checks that a module is ready to be replaced by a
// converted or handcrafted Bazel target.
func (b *BazelModuleBase) MixedBuildsEnabled(ctx BazelConversionPathContext) bool {
	if !ctx.Config().BazelContext.BazelEnabled() {
		return false
	}
	if len(b.GetBazelLabel(ctx, ctx.Module())) == 0 {
		return false
	}
	return !mixedBuildsDisabled[ctx.Module().Name()]
}

// ConvertWithBp2build returns whether the given BazelModuleBase should be converted with bp2build.
func (b *BazelModuleBase) ConvertWithBp2build(ctx BazelConversionPathContext) bool {
	if bp2buildModuleDoNotConvert[ctx.Module().Name()] {
		return false
	}

	// Ensure that the module type of this module has a bp2build converter. This
	// prevents mixed builds from using auto-converted modules just by matching
	// the package dir; it also has to have a bp2build mutator as well.
	if ctx.Config().bp2buildModuleTypeConfig[ctx.ModuleType()] == false {
		return false
	}

	packagePath := ctx.ModuleDir()
	config := ctx.Config().bp2buildPackageConfig

	// This is a tristate value: true, false, or unset.
	propValue := b.bazelProperties.Bazel_module.Bp2build_available
	if bp2buildDefaultTrueRecursively(packagePath, config) {
		// Allow modules to explicitly opt-out.
		return proptools.BoolDefault(propValue, true)
	}

	// Allow modules to explicitly opt-in.
	return proptools.BoolDefault(propValue, false)
}

// bp2buildDefaultTrueRecursively checks that the package contains a prefix from the
// set of package prefixes where all modules must be converted. That is, if the
// package is x/y/z, and the list contains either x, x/y, or x/y/z, this function will
// return true.
//
// However, if the package is x/y, and it matches a Bp2BuildDefaultFalse "x/y" entry
// exactly, this module will return false early.
//
// This function will also return false if the package doesn't match anything in
// the config.
func bp2buildDefaultTrueRecursively(packagePath string, config Bp2BuildConfig) bool {
	ret := false

	// Return exact matches in the config.
	if config[packagePath] == Bp2BuildDefaultTrueRecursively {
		return true
	}
	if config[packagePath] == Bp2BuildDefaultFalse {
		return false
	}

	// If not, check for the config recursively.
	packagePrefix := ""
	// e.g. for x/y/z, iterate over x, x/y, then x/y/z, taking the final value from the allowlist.
	for _, part := range strings.Split(packagePath, "/") {
		packagePrefix += part
		if config[packagePrefix] == Bp2BuildDefaultTrueRecursively {
			// package contains this prefix and this prefix should convert all modules
			return true
		}
		// Continue to the next part of the package dir.
		packagePrefix += "/"
	}

	return ret
}

// GetBazelBuildFileContents returns the file contents of a hand-crafted BUILD file if available or
// an error if there are errors reading the file.
// TODO(b/181575318): currently we append the whole BUILD file, let's change that to do
// something more targeted based on the rule type and target.
func (b *BazelModuleBase) GetBazelBuildFileContents(c Config, path, name string) (string, error) {
	if !strings.Contains(b.HandcraftedLabel(), path) {
		return "", fmt.Errorf("%q not found in bazel_module.label %q", path, b.HandcraftedLabel())
	}
	name = filepath.Join(path, name)
	f, err := c.fs.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()

	data, err := ioutil.ReadAll(f)
	if err != nil {
		return "", err
	}
	return string(data[:]), nil
}

// ConvertedToBazel returns whether this module has been converted to Bazel, whether automatically
// or manually
func (b *BazelModuleBase) ConvertedToBazel(ctx BazelConversionPathContext) bool {
	return b.ConvertWithBp2build(ctx) || b.HasHandcraftedLabel()
}