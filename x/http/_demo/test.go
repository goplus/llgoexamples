package main

import (
	"fmt"
	"io"
)

func main() {

	// 假设你有一个 []byte 数组
	data := []byte("This is some data that needs to be stored in Body.")

	// 创建一个 io.Pipe
	pr, pw := io.Pipe()

	// 启动一个 goroutine 将数据写入 io.Pipe 的写入端
	go func() {
		defer pw.Close() // 确保写入完成后关闭写入端

		if _, err := pw.Write(data); err != nil {
			fmt.Println("Error writing to pipe:", err)
			return
		}
	}()

	// 读取 Body 中的数据进行验证
	readData, err := io.ReadAll(pr)
	if err != nil {
		fmt.Println("Error reading from Body:", err)
		return
	}

	// 输出 Body 中的数据
	fmt.Println("Body content:", string(readData))
	//
	//http.Get()

	//r, w := io.Pipe()
	//
	//go func() {
	//	fmt.Fprint(w, "some io.Reader stream to be read\n")
	//	w.Close()
	//}()
	//
	//if _, err := io.Copy(os.Stdout, r); err != nil {
	//	log.Fatal(err)
	//}

	// 使用 http.Get 发送 GET 请求
	//resp, err := http.Get("https://www.baidu.com/")
	//if err != nil {
	//	fmt.Println("Error:", err)
	//	return
	//}
	//defer resp.Body.Close()
	//
	//body, err := io.ReadAll(resp.Body)
	//if err != nil {
	//	fmt.Println("Error reading response:", err)
	//	return
	//}
	//fmt.Println("GET Response:\n", string(body))

	//rawURL := "http://example.com:8080/path/to/resource?query=123#fragment"
	//parsedURL, err := url.Parse(rawURL)
	//if err != nil {
	//	fmt.Println("Error parsing URL:", err)
	//	return
	//}
	//
	//hostname := parsedURL.Hostname()
	//port := parsedURL.Port()
	//
	//uri := parsedURL.RequestURI()
	//
	//fmt.Println("Hostname:", hostname)
	//fmt.Println("Port:", port)
	//fmt.Println("URI:", uri)

	//// 使用 http.Post 发送 POST 请求上传文件
	//file, err := os.Open("path/to/your/file.jpg")
	//if err != nil {
	//	fmt.Println("Error opening file:", err)
	//	return
	//}
	//defer file.Close()
	//
	//var buf bytes.Buffer
	//writer := multipart.NewWriter(&buf)
	//_, err = writer.CreateFormFile("file", "file.jpg")
	//if err != nil {
	//	fmt.Println("Error creating form file:", err)
	//	return
	//}
	//
	//_, err = io.ReadAll(file)
	//if err != nil {
	//	fmt.Println("Error reading file:", err)
	//	return
	//}
	//
	//err = writer.Close()
	//if err != nil {
	//	fmt.Println("Error closing writer:", err)
	//	return
	//}
	//
	//resp, err = http.Post("https://www.baidu.com/upload", writer.FormDataContentType(), &buf)
	//if err != nil {
	//	fmt.Println("Error:", err)
	//	return
	//}
	//defer resp.Body.Close()
	//
	//body, err = io.ReadAll(resp.Body)
	//if err != nil {
	//	fmt.Println("Error reading response:", err)
	//	return
	//}
	//fmt.Println("POST Response:\n", string(body))
	//
	//// 使用 http.PostForm 发送表单数据
	//formData := url.Values{
	//	"key": {"Value"},
	//	"id":  {"123"},
	//}
	//
	//resp, err = http.PostForm("https://www.baidu.com/form", formData)
	//if err != nil {
	//	fmt.Println("Error:", err)
	//	return
	//}
	//defer resp.Body.Close()
	//
	//body, err = io.ReadAll(resp.Body)
	//if err != nil {
	//	fmt.Println("Error reading response:", err)
	//	return
	//}
	//fmt.Println("POST Form Response:\n", string(body))
}
