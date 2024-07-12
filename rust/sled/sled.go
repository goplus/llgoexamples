package sled

import (
	"unsafe"

	"github.com/goplus/llgo/c"
)

// Write the .pc file for the dylib generated by the Rust library and copy it to pkg-config for proper location.
const (
	LLGoPackage = "link: $(pkg-config --libs sled); -lsled"
)

// -----------------------------------------------------------------------------

// Free a buffer originally allocated by sled database.
//
//go:linkname FreeBuf C.sled_free_buf
func FreeBuf(buf *byte, sz uintptr)

// Value represents a value returns by sled database.
type Value string

func (v Value) Free() {
	FreeBuf(unsafe.StringData(string(v)), uintptr(len(v)))
}

// -----------------------------------------------------------------------------

type Config struct {
	Unused [8]byte
}

// Create a new configuration.
//
//go:linkname NewConfig C.sled_create_config
func NewConfig() *Config

// Free a configuration.
//
// llgo:link (*Config).Free C.sled_free_config
func (conf *Config) Free() {}

// Set the configured file path.
// The caller is responsible for freeing the path string after calling
// this (it is copied in this function).
//
// llgo:link (*Config).SetPath C.sled_config_set_path
func (conf *Config) SetPath(path *c.Char) *Config { return nil }

// Set the configured cache capacity in bytes.
//
// llgo:link (*Config).SetCacheCapacity C.sled_config_set_cache_capacity
func (conf *Config) SetCacheCapacity(capacity uintptr) *Config { return nil }

// Set the configured IO buffer flush interval in milliseconds.
//
// llgo:link (*Config).SetFlushEveryMs C.sled_config_flush_every_ms
func (conf *Config) SetFlushEveryMs(flushEveryMs c.Int) *Config { return nil }

// Configure the use of the zstd compression library.
//
// llgo:link (*Config).UseCompression C.sled_config_use_compression
func (conf *Config) UseCompression(useCompression byte) *Config { return nil }

// -----------------------------------------------------------------------------

// DB represents a sled database.
// Its implementation is based on lock-free log-structured tree.
type DB struct {
	Unused [8]byte
}

// Open a sled database.
//
//go:linkname Open C.sled_open_db
func Open(conf *Config) *DB

// Close a sled database.
//
// llgo:link (*DB).Close C.sled_close
func (db *DB) Close() {}

// Set a key to a value
//
// llgo:link (*DB).Set C.sled_set
func (db *DB) Set(key *byte, keyLen uintptr, val *byte, valLen uintptr) {}

// SetBytes sets a key to a value.
func (db *DB) SetBytes(key, val []byte) {
	db.Set(unsafe.SliceData(key), uintptr(len(key)), unsafe.SliceData(val), uintptr(len(val)))
}

// SetString sets a key to a value.
func (db *DB) SetString(key, val string) {
	db.Set(unsafe.StringData(key), uintptr(len(key)), unsafe.StringData(val), uintptr(len(val)))
}

// Get gets the value of a key.
//
// llgo:link (*DB).Get C.sled_get
func (db *DB) Get(key *byte, keyLen uintptr, valLen *uintptr) *byte { return nil }

// GetBytes gets the value of a key.
func (db *DB) GetBytes(key []byte) Value {
	var valLen uintptr
	val := db.Get(unsafe.SliceData(key), uintptr(len(key)), &valLen)
	return Value(unsafe.String(val, valLen))
}

// GetString gets the value of a key.
func (db *DB) GetString(key string) Value {
	var valLen uintptr
	val := db.Get(unsafe.StringData(key), uintptr(len(key)), &valLen)
	return Value(unsafe.String(val, valLen))
}

// Delete deletes the value of a key.
//
// llgo:link (*DB).Del C.sled_del
func (db *DB) Del(key *byte, keyLen uintptr) {}

// DelBytes deletes the value of a key.
func (db *DB) DelBytes(key []byte) {
	db.Del(unsafe.SliceData(key), uintptr(len(key)))
}

// DelString deletes the value of a key.
func (db *DB) DelString(key string) {
	db.Del(unsafe.StringData(key), uintptr(len(key)))
}

// CAS compares and swaps the value of a key.
//
// llgo:link (*DB).CAS C.sled_compare_and_swap
func (db *DB) CAS(
	key *byte, keyLen uintptr, oldVal *byte, oldValLen uintptr,
	newVal *byte, newValLen uintptr, actualVal **byte, actualValLen *uintptr,
) bool {
	return false
}

// CASBytes compares and swaps the value of a key.
func (db *DB) CASBytes(key, oldVal, newVal []byte, actual *Value) bool {
	var pactualVal **byte
	var actualVal *byte
	var actualValLen uintptr
	if actual != nil {
		pactualVal = &actualVal
	}
	ret := db.CAS(
		unsafe.SliceData(key), uintptr(len(key)),
		unsafe.SliceData(oldVal), uintptr(len(oldVal)),
		unsafe.SliceData(newVal), uintptr(len(newVal)),
		pactualVal, &actualValLen)
	if actual != nil {
		*actual = Value(unsafe.String(actualVal, actualValLen))
	}
	return ret
}

// CASString compares and swaps the value of a key.
func (db *DB) CASString(key, oldVal, newVal string, actual *Value) bool {
	var pactualVal **byte
	var actualVal *byte
	var actualValLen uintptr
	if actual != nil {
		pactualVal = &actualVal
	}
	ret := db.CAS(
		unsafe.StringData(key), uintptr(len(key)),
		unsafe.StringData(oldVal), uintptr(len(oldVal)),
		unsafe.StringData(newVal), uintptr(len(newVal)),
		pactualVal, &actualValLen)
	if actual != nil {
		*actual = Value(unsafe.String(actualVal, actualValLen))
	}
	return ret
}

// -----------------------------------------------------------------------------
