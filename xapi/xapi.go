// Copyright 2016 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package xapi

import (
	"io"
	"io/ioutil"

	"github.com/golang/protobuf/proto"
	"github.com/juju/errors"
	"github.com/pingcap/tidb/kv"
	"github.com/pingcap/tidb/util/codec"
	"github.com/pingcap/tidb/util/types"
	"github.com/pingcap/tidb/xapi/tablecodec"
	"github.com/pingcap/tidb/xapi/tipb"
)

// SelectResult is used to get response rows from SelectRequest.
type SelectResult struct {
	fields []*types.FieldType
	resp   kv.Response
}

// Next returns the next row.
func (r *SelectResult) Next() (subResult *SubResult, err error) {
	var reader io.ReadCloser
	reader, err = r.resp.Next()
	if err != nil {
		return nil, errors.Trace(err)
	}
	if reader == nil {
		return nil, nil
	}
	subResult = &SubResult{
		fields: r.fields,
		reader: reader,
	}
	return
}

// Close closes SelectResult.
func (r *SelectResult) Close() error {
	return r.resp.Close()
}

// SubResult represents a subset of select result.
type SubResult struct {
	fields []*types.FieldType
	reader io.ReadCloser
	resp   *tipb.SelectResponse
	cursor int
}

// Next returns the next row of the sub result.
// If no more row to return, data would be nil.
func (r *SubResult) Next() (handle int64, data []types.Datum, err error) {
	if r.resp == nil {
		r.resp = new(tipb.SelectResponse)
		var b []byte
		b, err = ioutil.ReadAll(r.reader)
		r.reader.Close()
		if err != nil {
			return 0, nil, errors.Trace(err)
		}
		err = proto.Unmarshal(b, r.resp)
		if err != nil {
			return 0, nil, errors.Trace(err)
		}
	}
	if r.cursor >= len(r.resp.Rows) {
		return 0, nil, nil
	}
	row := r.resp.Rows[r.cursor]
	data, err = tablecodec.DecodeValues(row.Data, r.fields)
	if err != nil {
		return 0, nil, errors.Trace(err)
	}
	handleBytes := row.GetHandle()
	_, handle, err = codec.DecodeInt(handleBytes)
	if err != nil {
		return 0, nil, errors.Trace(err)
	}
	r.cursor++
	return
}

// Close closes the sub result.
func (r *SubResult) Close() error {
	return nil
}

// Select do a select request, returns SelectResult.
func Select(client kv.Client, req *tipb.SelectRequest, concurrency int) (*SelectResult, error) {
	// Convert tipb.*Request to kv.Request
	kvReq, err := composeRequest(req, concurrency)
	if err != nil {
		return nil, errors.Trace(err)
	}
	resp := client.Send(kvReq)
	if resp == nil {
		return nil, errors.New("client returns nil response")
	}
	var columns []*tipb.ColumnInfo
	if req.TableInfo != nil {
		columns = req.TableInfo.Columns
	} else {
		columns = req.IndexInfo.Columns
	}
	fields := tablecodec.ProtoColumnsToFieldTypes(columns)
	return &SelectResult{fields: fields, resp: resp}, nil
}

// Convert tipb.Request to kv.Request.
func composeRequest(req *tipb.SelectRequest, concurrency int) (*kv.Request, error) {
	kvReq := &kv.Request{
		Concurrency: concurrency,
	}
	if req.IndexInfo != nil {
		kvReq.Tp = kv.ReqTypeIndex
		tid := req.IndexInfo.GetTableId()
		kvReq.KeyRanges = tablecodec.EncodeIndexRanges(tid, req.Ranges, req.Points)
	} else {
		kvReq.Tp = kv.ReqTypeSelect
		tid := req.GetTableInfo().GetTableId()
		kvReq.KeyRanges = tablecodec.EncodeTableRanges(tid, req.Ranges, req.Points)
	}
	var err error
	kvReq.Data, err = proto.Marshal(req)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return kvReq, nil
}

// SupportExpression checks if the expression is supported by the client.
func SupportExpression(client kv.Client, expr *tipb.Expr) bool {
	return false
}
