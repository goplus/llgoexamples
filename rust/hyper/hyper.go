package hyper

import (
	_ "unsafe"

	"github.com/goplus/llgo/c"
)

const (
	LLGoPackage = "link: $(pkg-config --libs hyper); -lhyper"
)

const (
	IterContinue = 0
	IterBreak    = 1
	IoPending    = 4294967295
	IoError      = 4294967294
	PollReady    = 0
	PollPending  = 1
	PollError    = 3
)

type HTTPVersion c.Int

const (
	HTTPVersionNone HTTPVersion = 0
	HTTPVersion10   HTTPVersion = 10
	HTTPVersion11   HTTPVersion = 11
	HTTPVersion2    HTTPVersion = 20
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
	InsufficientSpace
)

type TaskReturnType c.Int

const (
	TaskEmpty TaskReturnType = iota
	TaskError
	TaskClientConn
	TaskResponse
	TaskBuf
	TaskServerconn
)

type ExampleId c.Int

const (
	ExampleNotSet ExampleId = iota
	ExampleHandshake
	ExampleSend
	ExampleRespBody
)

type Body struct {
	Unused [0]byte
}

type Buf struct {
	Unused [0]byte
}

type ClientConn struct {
	Unused [0]byte
}

type ClientConnOptions struct {
	Unused [0]byte
}

type Context struct {
	Unused [0]byte
}

type Error struct {
	Unused [0]byte
}

type Executor struct {
	Unused [0]byte
}

type Headers struct {
	Unused [0]byte
}

type Io struct {
	Unused [0]byte
}

type Request struct {
	Unused [0]byte
}

type Response struct {
	Unused [0]byte
}

type Task struct {
	Unused [0]byte
}

type Waker struct {
	Unused [0]byte
}

type Http1ServerconnOptions struct {
	Unused [0]byte
}

type Http2ServerconnOptions struct {
	Unused [0]byte
}

type ResponseChannel struct {
	Unused [0]byte
}

type Service struct {
	Unused [0]byte
}

// llgo:type C
type BodyForeachCallback func(c.Pointer, *Buf) c.Int

// llgo:type C
type BodyDataCallback func(c.Pointer, *Context, **Buf) c.Int

// llgo:type C
type RequestOnInformationalCallback func(c.Pointer, *Response)

// llgo:type C
type HeadersForeachCallback func(c.Pointer, *byte, uintptr, *byte, uintptr) c.Int

// llgo:type C
type IoReadCallback func(c.Pointer, *Context, *byte, uintptr) uintptr

// llgo:type C
type IoWriteCallback func(c.Pointer, *Context, *byte, uintptr) uintptr

//llgo:type C
type UserdataDrop func(c.Pointer)

//llgo:type C
type ServiceCallback func(c.Pointer, *Request, *ResponseChannel)

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
func (body *Body) Free() {}

// Creates a task that will poll a response body for the next buffer of data.
// llgo:link (*Body).Data C.hyper_body_data
func (body *Body) Data() *Task {
	return nil
}

// Creates a task to execute the callback with each body chunk received.
// llgo:link (*Body).Foreach C.hyper_body_foreach
func (body *Body) Foreach(callback BodyForeachCallback, userdata c.Pointer, drop UserdataDrop) *Task {
	return nil
}

// Set userdata on this body, which will be passed to callback functions.
// llgo:link (*Body).SetUserdata C.hyper_body_set_userdata
func (body *Body) SetUserdata(userdata c.Pointer, drop UserdataDrop) {}

// Set the outgoing data callback for this body.
// llgo:link (*Body).SetDataFunc C.hyper_body_set_data_func
func (body *Body) SetDataFunc(callback BodyDataCallback) {}

// Create a new `hyper_buf *` by copying the provided bytes.
// llgo:link CopyBuf C.hyper_buf_copy
func CopyBuf(buf *byte, len uintptr) *Buf {
	return nil
}

// Get a pointer to the bytes in this buffer.
// llgo:link (*Buf).Bytes C.hyper_buf_bytes
func (buf *Buf) Bytes() *byte {
	return nil
}

// Get the length of the bytes this buffer contains.
// llgo:link (*Buf).Len C.hyper_buf_len
func (buf *Buf) Len() uintptr {
	return 0
}

// Free this buffer.
// llgo:link (*Buf).Free C.hyper_buf_free
func (buf *Buf) Free() {}

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
func (conn *ClientConn) Free() {}

// Creates a new set of HTTP clientconn options to be used in a handshake.
// llgo:link NewClientConnOptions C.hyper_clientconn_options_new
func NewClientConnOptions() *ClientConnOptions {
	return nil
}

// Set whether header case is preserved.
// llgo:link (*ClientConnOptions).SetPreserveHeaderCase C.hyper_clientconn_options_set_preserve_header_case
func (opts *ClientConnOptions) SetPreserveHeaderCase(enabled c.Int) {}

// Set whether header order is preserved.
// llgo:link (*ClientConnOptions).SetPreserveHeaderOrder C.hyper_clientconn_options_set_preserve_header_order
func (opts *ClientConnOptions) SetPreserveHeaderOrder(enabled c.Int) {}

// Free a set of HTTP clientconn options.
// llgo:link (*ClientConnOptions).Free C.hyper_clientconn_options_free
func (opts *ClientConnOptions) Free() {}

// Set the client background task executor.
// llgo:link (*ClientConnOptions).Exec C.hyper_clientconn_options_exec
func (opts *ClientConnOptions) Exec(exec *Executor) {}

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
func (err *Error) Print(dst *byte, dstLen uintptr) uintptr {
	return 0
}

// Construct a new HTTP request.
// llgo:link NewRequest C.hyper_request_new
func NewRequest() *Request {
	return nil
}

// Free an HTTP request.
// llgo:link (*Request).Free C.hyper_request_free
func (req *Request) Free() {}

// Set the HTTP Method of the request.
// llgo:link (*Request).SetMethod C.hyper_request_set_method
func (req *Request) SetMethod(method *byte, methodLen uintptr) Code {
	return 0
}

// Set the URI of the request.
// llgo:link (*Request).SetURI C.hyper_request_set_uri
func (req *Request) SetURI(uri *byte, uriLen uintptr) Code {
	return 0
}

// Set the URI of the request with separate scheme, authority, and
// llgo:link (*Request).SetURIParts C.hyper_request_set_uri_parts
func (req *Request) SetURIParts(scheme *byte, schemeLen uintptr, authority *byte, authorityLen uintptr, pathAndQuery *byte, pathAndQueryLen uintptr) Code {
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
func (req *Request) OnInformational(callback RequestOnInformationalCallback, data c.Pointer, drop UserdataDrop) Code {
	return 0
}

// Method get the HTTP Method of the request.
// llgo:link (*Request).Method C.hyper_request_method
func (req *Request) Method(method *byte, methodLen *uintptr) Code {
	return 0
}

// URIParts get the URI of the request split into scheme, authority and path/query strings.
// llgo:link (*Request).URIParts C.hyper_request_uri_parts
func (req *Request) URIParts(scheme *byte, schemeLen *uintptr, authority *byte, authorityLen *uintptr, pathAndQuery *byte, pathAndQueryLen *uintptr) Code {
	return 0
}

// Version set the preferred HTTP version of the request.
// llgo:link (*Request).Version C.hyper_request_version
func (req *Request) Version() HTTPVersion {
	return 0
}

// Body take ownership of the body of this request.
// llgo:link (*Request).Body C.hyper_request_body
func (req *Request) Body() *Body {
	return nil
}

// New construct a new HTTP 200 Ok response
//
//go:linkname NewResponse C.hyper_response_new
func NewResponse() *Response {
	return nil
}

// SetBody set the body of the response.
// llgo:link (*Response).SetBody C.hyper_response_set_body
func (resp *Response) SetBody(body *Body) Code {
	return 0
}

// Free an HTTP response.
// llgo:link (*Response).Free C.hyper_response_free
func (resp *Response) Free() {}

// Get the HTTP-Status code of this response.
// llgo:link (*Response).Status C.hyper_response_status
func (resp *Response) Status() uint16 {
	return 0
}

// SetStatus sets the HTTP Status-Code of this response.
// llgo:link (*Response).SetStatus C.hyper_response_set_status
func (resp *Response) SetStatus(status uint16) {
}

// Get a pointer to the reason-phrase of this response.
// llgo:link (*Response).ReasonPhrase C.hyper_response_reason_phrase
func (resp *Response) ReasonPhrase() *byte {
	return nil
}

// Get the length of the reason-phrase of this response.
// llgo:link (*Response).ReasonPhraseLen C.hyper_response_reason_phrase_len
func (resp *Response) ReasonPhraseLen() byte {
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
func (headers *Headers) Foreach(callback HeadersForeachCallback, userdata c.Pointer) {}

// Sets the header with the provided name to the provided value.
// llgo:link (*Headers).Set C.hyper_headers_set
func (headers *Headers) Set(name *byte, nameLen uintptr, value *byte, valueLen uintptr) Code {
	return 0
}

// Adds the provided value to the list of the provided name.
// llgo:link (*Headers).Add C.hyper_headers_add
func (headers *Headers) Add(name *byte, nameLen uintptr, value *byte, valueLen uintptr) Code {
	return 0
}

// Create a new IO type used to represent a transport.
// llgo:link NewIo C.hyper_io_new
func NewIo() *Io {
	return nil
}

// Free an IO handle.
// llgo:link (*Io).Free C.hyper_io_free
func (io *Io) Free() {}

// Set the user data pointer for this IO to some value.
// llgo:link (*Io).SetUserdata C.hyper_io_set_userdata
func (io *Io) SetUserdata(data c.Pointer, drop UserdataDrop) {}

// Set the read function for this IO transport.
// llgo:link (*Io).SetRead C.hyper_io_set_read
func (io *Io) SetRead(callback IoReadCallback) {}

// Set the write function for this IO transport.
// llgo:link (*Io).SetWrite C.hyper_io_set_write
func (io *Io) SetWrite(callback IoWriteCallback) {}

// GetUserdata set the user data pointer for this IO to some value.
// llgo:link (*Io).GetUserdata C.hyper_io_get_userdata
func (io *Io) GetUserdata() c.Pointer {
	return nil
}

// Creates a new task executor.
// llgo:link NewExecutor C.hyper_executor_new
func NewExecutor() *Executor {
	return nil
}

// Frees an executor and any incomplete tasks still part of it.
// llgo:link (*Executor).Free C.hyper_executor_free
func (exec *Executor) Free() {}

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

// NextTimerPop returns the time until the executor will be able to make progress on tasks due to internal timers.
// llgo:link (*Executor).NextTimerPop C.hyper_executor_next_timer_pop
func (exec *Executor) NextTimerPop() c.Int {
	return 0
}

// Free a task.
// llgo:link (*Task).Free C.hyper_task_free
func (task *Task) Free() {}

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
func (task *Task) SetUserdata(userdata c.Pointer, drop UserdataDrop) {}

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
func (waker *Waker) Free() {}

// Wake up the task associated with a waker.
// llgo:link (*Waker).Wake C.hyper_waker_wake
func (waker *Waker) Wake() {}

// New create a new HTTP/1 serverconn options object.
//
//go:linkname Http1ServerconnOptionsNew C.hyper_http1_serverconn_options_new
func Http1ServerconnOptionsNew(executor *Executor) *Http1ServerconnOptions {
	return nil
}

// Free a `Http1ServerconnOptions`.
// llgo:link (*Http1ServerconnOptions).Free C.hyper_http1_serverconn_options_free
func (opts *Http1ServerconnOptions) Free() {}

// HalfClose set whether HTTP/1 connections should support half-closures.
// llgo:link (*Http1ServerconnOptions).HalfClose C.hyper_http1_serverconn_options_half_close
func (opts *Http1ServerconnOptions) HalfClose(enabled bool) Code {
	return 0
}

// KeepAlive enables or disables HTTP/1 keep-alive.
// llgo:link (*Http1ServerconnOptions).KeepAlive C.hyper_http1_serverconn_options_keep_alive
func (opts *Http1ServerconnOptions) KeepAlive(enabled bool) Code {
	return 0
}

// TitleCaseHeaders set whether HTTP/1 connections will write header names as title case at the socket level.
// llgo:link (*Http1ServerconnOptions).TitleCaseHeaders C.hyper_http1_serverconn_options_title_case_headers
func (opts *Http1ServerconnOptions) TitleCaseHeaders(enabled bool) Code {
	return 0
}

// HeaderReadTimeout sets a timeout for reading client request headers.
// If a client does not send a complete set of headers within this time, the connection will be closed.
// llgo:link (*Http1ServerconnOptions).HeaderReadTimeout C.hyper_http1_serverconn_options_header_read_timeout
func (opts *Http1ServerconnOptions) HeaderReadTimeout(millis c.UlongLong) Code {
	return 0
}

// Writev sets whether HTTP/1 connections should try to use vectored writes, or always flatten into a single buffer.
// llgo:link (*Http1ServerconnOptions).Writev C.hyper_http1_serverconn_options_writev
func (opts *Http1ServerconnOptions) Writev(enabled bool) Code {
	return 0
}

// MaxBufSize sets the maximum buffer size for the HTTP/1 connection. Must be no lower than 8192.
// llgo:link (*Http1ServerconnOptions).MaxBufSize C.hyper_http1_serverconn_options_max_buf_size
func (opts *Http1ServerconnOptions) MaxBufSize(maxBufSize c.Ulong) Code {
	return 0
}

// PipelineFlush aggregates flushes to better support pipelined responses.
// llgo:link (*Http1ServerconnOptions).PipelineFlush C.hyper_http1_serverconn_options_pipeline_flush
func (opts *Http1ServerconnOptions) PipelineFlush(enabled bool) Code {
	return 0
}

// New creates a new HTTP/2 serverconn options object bound to the provided executor.
//
//go:linkname Http2ServerconnOptionsNew C.hyper_http2_serverconn_options_new
func Http2ServerconnOptionsNew(exec *Executor) *Http2ServerconnOptions {
	return nil
}

// Free releases resources associated with the Http2ServerconnOptions.
// llgo:link (*Http2ServerconnOptions).Free C.hyper_http2_serverconn_options_free
func (opts *Http2ServerconnOptions) Free() {}

// InitialStreamWindowSize sets the `SETTINGS_INITIAL_WINDOW_SIZE` option for HTTP/2 stream-level flow control.
// llgo:link (*Http2ServerconnOptions).InitialStreamWindowSize C.hyper_http2_serverconn_options_initial_stream_window_size
func (opts *Http2ServerconnOptions) InitialStreamWindowSize(windowSize c.Uint) Code {
	return 0
}

// InitialConnectionWindowSize sets the max connection-level flow control for HTTP/2.
// llgo:link (*Http2ServerconnOptions).InitialConnectionWindowSize C.hyper_http2_serverconn_options_initial_connection_window_size
func (opts *Http2ServerconnOptions) InitialConnectionWindowSize(windowSize c.Uint) Code {
	return 0
}

// AdaptiveWindow sets whether to use an adaptive flow control.
// llgo:link (*Http2ServerconnOptions).AdaptiveWindow C.hyper_http2_serverconn_options_adaptive_window
func (opts *Http2ServerconnOptions) AdaptiveWindow(enabled bool) Code {
	return 0
}

// MaxFrameSize sets the maximum frame size to use for HTTP/2.
// llgo:link (*Http2ServerconnOptions).MaxFrameSize C.hyper_http2_serverconn_options_max_frame_size
func (opts *Http2ServerconnOptions) MaxFrameSize(frameSize c.Uint) Code {
	return 0
}

// MaxConcurrentStreams sets the `SETTINGS_MAX_CONCURRENT_STREAMS` option for HTTP/2 connections.
// llgo:link (*Http2ServerconnOptions).MaxConcurrentStreams C.hyper_http2_serverconn_options_max_concurrent_streams
func (opts *Http2ServerconnOptions) MaxConcurrentStreams(maxStreams c.Uint) Code {
	return 0
}

// KeepAliveInterval sets an interval for HTTP/2 Ping frames should be sent to keep a connection alive.
// llgo:link (*Http2ServerconnOptions).KeepAliveInterval C.hyper_http2_serverconn_options_keep_alive_interval
func (opts *Http2ServerconnOptions) KeepAliveInterval(intervalSeconds c.UlongLong) Code {
	return 0
}

// KeepAliveTimeout sets a timeout for receiving an acknowledgement of the keep-alive ping.
// llgo:link (*Http2ServerconnOptions).KeepAliveTimeout C.hyper_http2_serverconn_options_keep_alive_timeout
func (opts *Http2ServerconnOptions) KeepAliveTimeout(timeoutSeconds c.UlongLong) Code {
	return 0
}

// MaxSendBufSize sets the maximum write buffer size for each HTTP/2 stream.
// llgo:link (*Http2ServerconnOptions).MaxSendBufSize C.hyper_http2_serverconn_options_max_send_buf_size
func (opts *Http2ServerconnOptions) MaxSendBufSize(maxBufSize c.Ulong) Code {
	return 0
}

// EnableConnectProtocol enables the extended `CONNECT` protocol.
// llgo:link (*Http2ServerconnOptions).EnableConnectProtocol C.hyper_http2_serverconn_options_enable_connect_protocol
func (opts *Http2ServerconnOptions) EnableConnectProtocol() Code {
	return 0
}

// MaxHeaderListSize sets the max size of received header frames.
// llgo:link (*Http2ServerconnOptions).MaxHeaderListSize C.hyper_http2_serverconn_options_max_header_list_size
func (opts *Http2ServerconnOptions) MaxHeaderListSize(max c.Uint) Code {
	return 0
}

// New creates a service from a wrapped callback function.
//
//go:linkname ServiceNew C.hyper_service_new
func ServiceNew(serviceFn ServiceCallback) *Service {
	return nil
}

// SetUserdata registers opaque userdata with the Service.
// This userdata must be Send in a rust
// llgo:link (*Service).SetUserdata C.hyper_service_set_userdata
func (s *Service) SetUserdata(userdata c.Pointer, drop UserdataDrop) {
}

// Free frees a Service object if no longer needed
// llgo:link (*Service).Free C.hyper_service_free
func (s *Service) Free() {
}

// ServeHttp1Connection serves the provided Service as an HTTP/1 endpoint over the provided IO
// llgo:link ServeHttp1Connection C.hyper_serve_http1_connection
func ServeHttp1Connection(serverconnOptions *Http1ServerconnOptions, io *Io, service *Service) *Task {
	return nil
}

// ServeHttp2Connection serves the provided Service as an HTTP/2 endpoint over the provided IO
// llgo:link ServeHttp2Connection C.hyper_serve_http2_connection
func ServeHttp2Connection(serverconnOptions *Http2ServerconnOptions, io *Io, service *Service) *Task {
	return nil
}

// ServeHttpXConnection serves the provided Service as either an HTTP/1 or HTTP/2 (depending on what the client supports) endpoint over the provided IO
// llgo:link ServeHttpXConnection C.hyper_serve_httpX_connection
func ServeHttpXConnection(http1ServerconnOptions *Http1ServerconnOptions, http2ServerconnOptions *Http2ServerconnOptions, io *Io, service *Service) *Task {
	return nil
}

// Send sends a Response back to the client. This function consumes the response and the channel.
// llgo:link (*ResponseChannel).Send C.hyper_response_channel_send
func (rc *ResponseChannel) Send(response *Response) {
}
