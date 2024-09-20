package main

import (
	"fmt"
	"sync"

	"github.com/goplus/llgoexamples/x/net/http"
)

func worker(id int, wg *sync.WaitGroup) {
	defer wg.Done()
	resp, err := http.Get("http://www.baidu.com")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(id, ":", resp.Status)
	//body, err := io.ReadAll(resp.Body)
	//if err != nil {
	//	fmt.Println(err)
	//	return
	//}
	//fmt.Println(string(body))
	resp.Body.Close()
}

func main() {
	var wait sync.WaitGroup
	for i := 0; i < 500; i++ {
		wait.Add(1)
		go worker(i, &wait)
	}
	wait.Wait()
	fmt.Println("All done")

	resp, err := http.Get("http://www.baidu.com")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(resp.Status)
	resp.Body.Close()
}
