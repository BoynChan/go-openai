package openai

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"

	utils "github.com/BoynChan/go-openai/internal"
)

var (
	headerData  = []byte("data: ")
	errorPrefix = []byte(`data: {"error":`)
)

type Streamable interface {
	ChatCompletionStreamResponse | CompletionResponse
}

type StreamReader[T Streamable] struct {
	EmptyMessagesLimit uint
	isFinished         bool

	Reader         *bufio.Reader
	Response       *http.Response
	ErrAccumulator utils.ErrorAccumulator
	Unmarshaler    utils.Unmarshaler
}

func (stream *StreamReader[T]) Recv() (response T, err error) {
	if stream.isFinished {
		err = io.EOF
		return
	}

	response, err = stream.processLines()
	return
}

//nolint:gocognit
func (stream *StreamReader[T]) processLines() (T, error) {
	var (
		emptyMessagesCount uint
		hasErrorPrefix     bool
	)

	for {
		rawLine, readErr := stream.Reader.ReadBytes('\n')
		if readErr != nil || hasErrorPrefix {
			respErr := stream.unmarshalError()
			if respErr != nil {
				return *new(T), fmt.Errorf("error, %w", respErr.Error)
			}
			return *new(T), readErr
		}

		noSpaceLine := bytes.TrimSpace(rawLine)
		if bytes.HasPrefix(noSpaceLine, errorPrefix) {
			hasErrorPrefix = true
		}
		if !bytes.HasPrefix(noSpaceLine, headerData) || hasErrorPrefix {
			if hasErrorPrefix {
				noSpaceLine = bytes.TrimPrefix(noSpaceLine, headerData)
			}
			writeErr := stream.ErrAccumulator.Write(noSpaceLine)
			if writeErr != nil {
				return *new(T), writeErr
			}
			emptyMessagesCount++
			if emptyMessagesCount > stream.EmptyMessagesLimit {
				return *new(T), ErrTooManyEmptyStreamMessages
			}

			continue
		}

		noPrefixLine := bytes.TrimPrefix(noSpaceLine, headerData)
		if string(noPrefixLine) == "[DONE]" {
			stream.isFinished = true
			return *new(T), io.EOF
		}

		var response T
		unmarshalErr := stream.Unmarshaler.Unmarshal(noPrefixLine, &response)
		if unmarshalErr != nil {
			return *new(T), unmarshalErr
		}

		return response, nil
	}
}

func (stream *StreamReader[T]) unmarshalError() (errResp *ErrorResponse) {
	errBytes := stream.ErrAccumulator.Bytes()
	if len(errBytes) == 0 {
		return
	}

	err := stream.Unmarshaler.Unmarshal(errBytes, &errResp)
	if err != nil {
		errResp = nil
	}

	return
}

func (stream *StreamReader[T]) Close() {
	stream.Response.Body.Close()
}
