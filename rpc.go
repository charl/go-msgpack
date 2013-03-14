
/*
go-msgpack - Msgpack library for Go. Provides pack/unpack and net/rpc support.
https://github.com/ugorji/go-msgpack

Copyright (c) 2012, 2013 Ugorji Nwoke.
All rights reserved.

Redistribution and use in source and binary forms, with or without modification,
are permitted provided that the following conditions are met:

* Redistributions of source code must retain the above copyright notice,
  this list of conditions and the following disclaimer.
* Redistributions in binary form must reproduce the above copyright notice,
  this list of conditions and the following disclaimer in the documentation
  and/or other materials provided with the distribution.
* Neither the name of the author nor the names of its contributors may be used
  to endorse or promote products derived from this software
  without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE FOR
ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
(INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON
ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
*/

/*
RPC

An RPC Client and Server Codec is implemented, so that msgpack can be used
with the standard net/rpc package. It supports both a basic net/rpc serialization,
and the custom format defined at http://wiki.msgpack.org/display/MSGPACK/RPC+specification.

*/
package msgpack

import (
	"fmt"
	"net/rpc"
	"io"
)

var (
	// GoRpc is the implementation of Rpc that uses basic msgpack serialization
	// as defined in net/rpc package for communication.
	GoRpc = goRpc{}
	// SpecRpc is the Rpc implementation that uses a custom protocol defined by
	// the msgpack spec at http://wiki.msgpack.org/display/MSGPACK/RPC+specification
	SpecRpc = specRpc{}
)

type Rpc interface {
	ServerCodec(conn io.ReadWriteCloser, eopts *EncoderOptions, dopts *DecoderOptions) (rpc.ServerCodec) 
	ClientCodec(conn io.ReadWriteCloser, eopts *EncoderOptions, dopts *DecoderOptions) (rpc.ClientCodec)
}

type goRpc struct {}
type specRpc struct{}

type rpcCodec struct {
	rwc       io.ReadWriteCloser
	dec       *Decoder
	enc       *Encoder
}

type goRpcCodec struct {
	rpcCodec
}

type specRpcCodec struct {
	rpcCodec
}

func newRPCCodec(conn io.ReadWriteCloser, eopts *EncoderOptions, dopts *DecoderOptions) (rpcCodec) {
	return rpcCodec{
		rwc: conn,
		dec: NewDecoder(conn, dopts),
		enc: NewEncoder(conn, eopts),
	}
}

func (goRpc) ServerCodec(conn io.ReadWriteCloser, eopts *EncoderOptions, dopts *DecoderOptions) (rpc.ServerCodec) {
	return &goRpcCodec{ newRPCCodec(conn, eopts, dopts) }
}

func (goRpc) ClientCodec(conn io.ReadWriteCloser, eopts *EncoderOptions, dopts *DecoderOptions) (rpc.ClientCodec) {
	return &goRpcCodec{ newRPCCodec(conn, eopts, dopts) }
}

func (specRpc) ServerCodec(conn io.ReadWriteCloser, eopts *EncoderOptions, dopts *DecoderOptions) (rpc.ServerCodec) {
	return &specRpcCodec{ newRPCCodec(conn, eopts, dopts) }
}

func (specRpc) ClientCodec(conn io.ReadWriteCloser, eopts *EncoderOptions, dopts *DecoderOptions) (rpc.ClientCodec) {
	return &specRpcCodec{ newRPCCodec(conn, eopts, dopts) }
}

// /////////////// RPC Codec Shared Methods ///////////////////
func (c *rpcCodec) write(objs ...interface{}) (err error) {
	for _, obj := range objs {
		if err = c.enc.Encode(obj); err != nil {
			return
		}
	}
	return
}

func (c *rpcCodec) read(objs ...interface{}) (err error) {
	for _, obj := range objs {
		if err = c.dec.Decode(obj); err != nil {
			return
		}
	}
	return
}

func (c *rpcCodec) Close() error {
	// fmt.Printf("Calling rpcCodec.Close: %v\n-----------\n", string(debug.Stack()))
	return c.rwc.Close()
	
}

func (c *rpcCodec) ReadResponseBody(body interface{}) error {
	return c.dec.Decode(body)
}

// /////////////// Basic RPC Codec ///////////////////
func (c *goRpcCodec) WriteRequest(r *rpc.Request, body interface{}) error {
	return c.write(r, body)
}

func (c *goRpcCodec) WriteResponse(r *rpc.Response, body interface{}) error {
	return c.write(r, body)
}

func (c *goRpcCodec) ReadRequestBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *goRpcCodec) ReadResponseHeader(r *rpc.Response) error {
	return c.dec.Decode(r)
}

func (c *goRpcCodec) ReadRequestHeader(r *rpc.Request) error {
	return c.dec.Decode(r)
}

// /////////////// Custom RPC Codec ///////////////////
func (c *specRpcCodec) WriteRequest(r *rpc.Request, body interface{}) error {
	return c.writeCustomBody(0, r.Seq, r.ServiceMethod, []interface{}{body})
}

func (c *specRpcCodec) WriteResponse(r *rpc.Response, body interface{}) error {
	return c.writeCustomBody(1, r.Seq, r.Error, body)
}

func (c *specRpcCodec) ReadRequestBody(body interface{}) error {
	bodyArr := []interface{}{body}
	return c.dec.Decode(&bodyArr)
}

func (c *specRpcCodec) ReadResponseHeader(r *rpc.Response) error {
	return c.parseCustomHeader(1, &r.Seq, &r.Error)
}

func (c *specRpcCodec) ReadRequestHeader(r *rpc.Request) error {
	return c.parseCustomHeader(0, &r.Seq, &r.ServiceMethod)
}

func (c *specRpcCodec) parseCustomHeader(expectTypeByte byte, msgid *uint64, methodOrError *string) (err error) {

	// We read the response header by hand 
	// so that the body can be decoded on its own from the stream at a later time.

	bs := make([]byte, 1)
	n, err := c.rwc.Read(bs)
	if err != nil {
		return 
	}
	if n != 1 {
		err = fmt.Errorf("Couldn't read array descriptor: No bytes read")
		return
	}
	const fia byte = 0x94 //four item array descriptor value
	if bs[0] != fia {
		err = fmt.Errorf("Unexpected value for array descriptor: Expecting %v. Received %v", fia, bs[0])
		return
	}
	var b byte
	if err = c.read(&b, msgid, methodOrError); err != nil {
		return
	}
	if b != expectTypeByte {
		err = fmt.Errorf("Unexpected byte descriptor in header. Expecting %v. Received %v", expectTypeByte, b)
		return
	}
	return
}

func (c *specRpcCodec) writeCustomBody(typeByte byte, msgid uint64, methodOrError string, body interface{}) (err error) {
	var moe interface{} = methodOrError
	// response needs nil error (not ""), and only one of error or body can be nil
	if typeByte == 1 {
		if methodOrError == "" {
			moe = nil
		}
		if moe != nil && body != nil {
			body = nil
		}
	}
	r2 := []interface{}{ typeByte, uint32(msgid), moe, body }
	return c.enc.Encode(r2)
}

