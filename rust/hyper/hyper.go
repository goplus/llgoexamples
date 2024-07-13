package hyper

import (
	_ "unsafe"

	"github.com/goplus/llgo/c"
)

const (
	LLGoPackage = "link: $(pkg-config --libs hyper); -lhyper"
)

const (
	IterContinue    = 0
	IterBreak       = 1
	HTTPVersionNone = 0
	HTTPVersion10   = 10
	HTTPVersion11   = 11
	HTTPVersion2    = 20
	IoPending       = 4294967295
	IoError         = 4294967294
	PollReady       = 0
	PollPending     = 1
	PollError       = 3
)

type Code c.Int

const (
	OK Code = iota
	ERROR
	InvalidArg
	UnexpectedEOF
	AbortedByCallback
	FeatureNotEnabled
	InvalidPeerMessage
)

type TaskReturnType c.Int

const (
	TaskEmpty TaskReturnType = iota
	TaskError
	TaskClientConn
	TaskResponse
	TaskBuf
)

type Body struct {
	Unused [8]byte
}

type Buf struct {
	Unused [8]byte
}

type ClientConn struct {
	Unused [8]byte
}

type ClientConnOptions struct {
	Unused [8]byte
}

type Context struct {
	Unused [8]byte
}

type Error struct {
	Unused [8]byte
}

type Executor struct {
	Unused [8]byte
}

type Headers struct {
	Unused [8]byte
}

type Io struct {
	Unused [8]byte
}

type Request struct {
	Unused [8]byte
}

type Response struct {
	Unused [8]byte
}

type Task struct {
	Unused [8]byte
}

type Waker struct {
	Unused [8]byte
}

type BodyForeachCallback func(c.Pointer, *Buf) c.Int

type BodyDataCallback func(c.Pointer, *Context, **Buf) c.Int

type RequestOnInformationalCallback func(c.Pointer, *Response)

type HeadersForeachCallback func(c.Pointer, *uint8, uintptr, *uint8, uintptr) c.Int

type IoReadCallback func(c.Pointer, *Context, *uint8, uintptr) uintptr

type IoWriteCallback func(c.Pointer, *Context, *uint8, uintptr) uintptr

// Returns a static ASCII (null terminated) string of the hyper_client version.
// llgo:link Version C.hyper_version
func Version() *c.Char { return nil }

// Creates a new "empty" body.
// llgo:link NewBody C.hyper_body_new
func NewBody() *Body {
	return nil
}

// Free a body.
// llgo:link (*Body).Free C.hyper_body_free
func (body *Body) Free() {
}

// Creates a task that will poll a response body for the next buffer of data.
// llgo:link (*Body).Data C.hyper_body_data
func (body *Body) Data() *Task {
	return nil
}

// Creates a task to execute the callback with each body chunk received.
// llgo:link (*Body).Foreach C.hyper_body_foreach
func (body *Body) Foreach(callback BodyForeachCallback, userdata c.Pointer) *Task {
	return nil
}

// Set userdata on this body, which will be passed to callback functions.
// llgo:link (*Body).SetUserdata C.hyper_body_set_userdata
func (body *Body) SetUserdata(userdata c.Pointer) {
}

// Set the outgoing data callback for this body.
// llgo:link (*Body).SetDataFunc C.hyper_body_set_data_func
func (body *Body) SetDataFunc(callback BodyDataCallback) {
}

// Create a new `hyper_buf *` by copying the provided bytes.
// llgo:link CopyBuf C.hyper_buf_copy
func CopyBuf(buf *uint8, len uintptr) *Buf {
	return nil
}

// Get a pointer to the bytes in this buffer.
// llgo:link (*Buf).Bytes C.hyper_buf_bytes
func (buf *Buf) Bytes() *uint8 {
	return nil
}

// Get the length of the bytes this buffer contains.
// llgo:link (*Buf).Len C.hyper_buf_len
func (buf *Buf) Len() uintptr {
	return 0
}

// Free this buffer.
// llgo:link (*Buf).Free C.hyper_buf_free
func (buf *Buf) Free() {
}

// Creates an HTTP client handshake task.
// llgo:link Handshake C.hyper_clientconn_handshake
func Handshake(io *Io, options *ClientConnOptions) *Task {
	return nil
}

// Creates a task to send a request on the client connection.
// llgo:link (*ClientConn).Send C.hyper_clientconn_send
func (conn *ClientConn) Send(req *Request) *Task {
	return nil
}

// Free a `hyper_clientconn *`.
// llgo:link (*ClientConn).Free C.hyper_clientconn_free
func (conn *ClientConn) Free() {
}

// Creates a new set of HTTP clientconn options to be used in a handshake.
// llgo:link NewClientConnOptions C.hyper_clientconn_options_new
func NewClientConnOptions() *ClientConnOptions {
	return nil
}

// Set whether header case is preserved.
// llgo:link (*ClientConnOptions).SetPreserveHeaderCase C.hyper_clientconn_options_set_preserve_header_case
func (opts *ClientConnOptions) SetPreserveHeaderCase(enabled c.Int) {
}

// Set whether header order is preserved.
// llgo:link (*ClientConnOptions).SetPreserveHeaderOrder C.hyper_clientconn_options_set_preserve_header_order
func (opts *ClientConnOptions) SetPreserveHeaderOrder(enabled c.Int) {
}

// Free a set of HTTP clientconn options.
// llgo:link (*ClientConnOptions).Free C.hyper_clientconn_options_free
func (opts *ClientConnOptions) Free() {
}

// Set the client background task executor.
// llgo:link (*ClientConnOptions).Exec C.hyper_clientconn_options_exec
func (opts *ClientConnOptions) Exec(exec *Executor) {
}

// Set whether to use HTTP2.
// llgo:link (*ClientConnOptions).HTTP2 C.hyper_clientconn_options_http2
func (opts *ClientConnOptions) HTTP2(enabled c.Int) Code {
	return 0
}

// Set whether HTTP/1 connections accept obsolete line folding for header values.
// llgo:link (*ClientConnOptions).HTTP1AllowMultilineHeaders C.hyper_clientconn_options_http1_allow_multiline_headers
func (opts *ClientConnOptions) HTTP1AllowMultilineHeaders(enabled c.Int) Code {
	return 0
}

// Frees a `hyper_error`.
// llgo:link (*Error).Free C.hyper_error_free
func (err *Error) Free() {
}

// Get an equivalent `c.Int` from this error.
// llgo:link (*Error).Code C.hyper_error_code
func (err *Error) Code() Code {
	return 0
}

// Print the details of this error to a buffer.
// llgo:link (*Error).Print C.hyper_error_print
func (err *Error) Print(dst *uint8, dstLen uintptr) uintptr {
	return 0
}

// Construct a new HTTP request.
// llgo:link NewRequest C.hyper_request_new
func NewRequest() *Request {
	return nil
}

// Free an HTTP request.
// llgo:link (*Request).Free C.hyper_request_free
func (req *Request) Free() {
}

// Set the HTTP Method of the request.
// llgo:link (*Request).SetMethod C.hyper_request_set_method
func (req *Request) SetMethod(method *uint8, methodLen uintptr) Code {
	return 0
}

// Set the URI of the request.
// llgo:link (*Request).SetURI C.hyper_request_set_uri
func (req *Request) SetURI(uri *uint8, uriLen uintptr) Code {
	return 0
}

// Set the URI of the request with separate scheme, authority, and
// llgo:link (*Request).SetURIParts C.hyper_request_set_uri_parts
func (req *Request) SetURIParts(scheme *uint8, schemeLen uintptr, authority *uint8, authorityLen uintptr, pathAndQuery *uint8, pathAndQueryLen uintptr) Code {
	return 0
}

// Set the preferred HTTP version of the request.
// llgo:link (*Request).SetVersion C.hyper_request_set_version
func (req *Request) SetVersion(version c.Int) Code {
	return 0
}

// Gets a mutable reference to the HTTP headers of this request.
// llgo:link (*Request).Headers C.hyper_request_headers
func (req *Request) Headers() *Headers {
	return nil
}

// Set the body of the request.
// llgo:link (*Request).SetBody C.hyper_request_set_body
func (req *Request) SetBody(body *Body) Code {
	return 0
}

// Set an informational (1xx) response callback.
// llgo:link (*Request).OnInformational C.hyper_request_on_informational
func (req *Request) OnInformational(callback RequestOnInformationalCallback, data c.Pointer) Code {
	return 0
}

// Free an HTTP response.
// llgo:link (*Response).Free C.hyper_response_free
func (resp *Response) Free() {
}

// Get the HTTP-Status code of this response.
// llgo:link (*Response).Status C.hyper_response_status
func (resp *Response) Status() uint16 {
	return 0
}

// Get a pointer to the reason-phrase of this response.
// llgo:link (*Response).ReasonPhrase C.hyper_response_reason_phrase
func (resp *Response) ReasonPhrase() *uint8 {
	return nil
}

// Get the length of the reason-phrase of this response.
// llgo:link (*Response).ReasonPhraseLen C.hyper_response_reason_phrase_len
func (resp *Response) ReasonPhraseLen() uintptr {
	return 0
}

// Get the HTTP version used by this response.
// llgo:link (*Response).Version C.hyper_response_version
func (resp *Response) Version() c.Int {
	return 0
}

// Gets a reference to the HTTP headers of this response.
// llgo:link (*Response).Headers C.hyper_response_headers
func (resp *Response) Headers() *Headers {
	return nil
}

// Take ownership of the body of this response.
// llgo:link (*Response).Body C.hyper_response_body
func (resp *Response) Body() *Body {
	return nil
}

// Iterates the headers passing each name and value pair to the callback.
// llgo:link (*Headers).Foreach C.hyper_headers_foreach
func (headers *Headers) Foreach(callback HeadersForeachCallback, userdata c.Pointer) {
}

// Sets the header with the provided name to the provided value.
// llgo:link (*Headers).Set C.hyper_headers_set
func (headers *Headers) Set(name *uint8, nameLen uintptr, value *uint8, valueLen uintptr) Code {
	return 0
}

// Adds the provided value to the list of the provided name.
// llgo:link (*Headers).Add C.hyper_headers_add
func (headers *Headers) Add(name *uint8, nameLen uintptr, value *uint8, valueLen uintptr) Code {
	return 0
}

// Create a new IO type used to represent a transport.
// llgo:link NewIo C.hyper_io_new
func NewIo() *Io {
	return nil
}

// Free an IO handle.
// llgo:link (*Io).Free C.hyper_io_free
func (io *Io) Free() {
}

// Set the user data pointer for this IO to some value.
// llgo:link (*Io).SetUserdata C.hyper_io_set_userdata
func (io *Io) SetUserdata(data c.Pointer) {
}

// Set the read function for this IO transport.
// llgo:link (*Io).SetRead C.hyper_io_set_read
func (io *Io) SetRead(callback IoReadCallback) {
}

// Set the write function for this IO transport.
// llgo:link (*Io).SetWrite C.hyper_io_set_write
func (io *Io) SetWrite(callback IoWriteCallback) {
}

// Creates a new task executor.
// llgo:link NewExecutor C.hyper_executor_new
func NewExecutor() *Executor {
	return nil
}

// Frees an executor and any incomplete tasks still part of it.
// llgo:link (*Executor).Free C.hyper_executor_free
func (exec *Executor) Free() {
}

// Push a task onto the executor.
// llgo:link (*Executor).Push C.hyper_executor_push
func (exec *Executor) Push(task *Task) Code {
	return 0
}

// Polls the executor, trying to make progress on any tasks that can do so.
// llgo:link (*Executor).Poll C.hyper_executor_poll
func (exec *Executor) Poll() *Task {
	return nil
}

// Free a task.
// llgo:link (*Task).Free C.hyper_task_free
func (task *Task) Free() {
}

// Takes the output value of this task.
// llgo:link (*Task).Value C.hyper_task_value
func (task *Task) Value() c.Pointer {
	return nil
}

// Query the return type of this task.
// llgo:link (*Task).Type C.hyper_task_type
func (task *Task) Type() TaskReturnType {
	return 0
}

// Set a user data pointer to be associated with this task.
// llgo:link (*Task).SetUserdata C.hyper_task_set_userdata
func (task *Task) SetUserdata(userdata c.Pointer) {
}

// Retrieve the userdata that has been set via `hyper_task_set_userdata`.
// llgo:link (*Task).Userdata C.hyper_task_userdata
func (task *Task) Userdata() c.Pointer {
	return nil
}

// Creates a waker associated with the task context.
// llgo:link (*Context).Waker C.hyper_context_waker
func (cx *Context) Waker() *Waker {
	return nil
}

// Free a waker.
// llgo:link (*Waker).Free C.hyper_waker_free
func (waker *Waker) Free() {
}

// Wake up the task associated with a waker.
// llgo:link (*Waker).Wake C.hyper_waker_wake
func (waker *Waker) Wake() {
}
