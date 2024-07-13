package main

import (
	"fmt"

	"github.com/goplus/llgo/c"
	"github.com/goplus/llgoexamples/rust/sled"
)

func main() {
	path := c.Str("./db")
	pathCopy := c.Strdup(path)
	conf := sled.NewConfig().SetPath(pathCopy)
	defer conf.Free()

	db := sled.Open(conf)
	db.SetString("key", "value")

	val := db.GetString("key")
	defer val.Free()

	fmt.Println("value:", val)
}
