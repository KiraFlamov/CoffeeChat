// Package httpapi — общий код точек входа cmd/server и cmd/container:
// обработчик ConnectRPC и служебные кадры WebSocket, которые собирает точка входа.
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	coffeechatv1 "git.sourcecraft.dev/drflamov/coffeechat/server/gen/coffeechat/v1"
	"git.sourcecraft.dev/drflamov/coffeechat/server/gen/coffeechat/v1/coffeechatv1connect"
	"git.sourcecraft.dev/drflamov/coffeechat/server/internal/core"
)

const (
	// maxRequestBytes — предел тела запроса HTTP API. Запросы этапа 0 — десятки байт.
	maxRequestBytes = 4 << 10
	// pingTimeout — срок проверки базы в Ping. Он короче таймаута запроса в облаке (10 с):
	// при зависшей базе клиент получает ответ с store_ok = false, а не обрыв.
	pingTimeout = 5 * time.Second
)

// Коды ошибок в кадре Error, которые выбирает точка входа (остальные — в core).
const (
	codeBadFrame = "bad_frame"
	codeInternal = "internal"
)

// NewSystemHandler возвращает путь и обработчик ConnectRPC для SystemService.
func NewSystemHandler(system *core.System) (string, http.Handler) {
	return coffeechatv1connect.NewSystemServiceHandler(
		&systemHandler{system: system},
		connect.WithReadMaxBytes(maxRequestBytes),
	)
}

type systemHandler struct {
	system *core.System
}

func (h *systemHandler) Ping(
	ctx context.Context, req *connect.Request[coffeechatv1.PingRequest],
) (*connect.Response[coffeechatv1.PingResponse], error) {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()

	res, err := h.system.Ping(ctx, req.Msg.GetNonce())
	if err != nil {
		if errors.Is(err, core.ErrNonceTooLarge) {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}
	return connect.NewResponse(&coffeechatv1.PingResponse{
		Nonce:      res.Nonce,
		Version:    res.Version,
		ServerTime: timestamppb.New(res.ServerTime),
		StoreOk:    res.StoreOK,
	}), nil
}

// BadFrameText — кадр-ответ на текстовое сообщение WebSocket: протокол только двоичный.
func BadFrameText() ([]byte, error) {
	return errorFrame(codeBadFrame, "text frames are not supported")
}

// InternalError — кадр-ответ на сбой базы или транспорта при обработке сообщения.
// Подробности остаются в журнале сервера, клиенту они не уходят.
func InternalError() ([]byte, error) {
	return errorFrame(codeInternal, "internal error")
}

func errorFrame(code, message string) ([]byte, error) {
	data, err := proto.Marshal(&coffeechatv1.ServerFrame{
		Payload: &coffeechatv1.ServerFrame_Error{
			Error: &coffeechatv1.Error{Code: code, Message: message},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("marshal error frame: %w", err)
	}
	return data, nil
}
