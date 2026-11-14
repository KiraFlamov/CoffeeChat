-- +goose Up
-- Открытые соединения WebSocket.
CREATE TABLE connections (
    connection_id Utf8 NOT NULL,      -- ID соединения от API Gateway
    connected_at  Timestamp NOT NULL,
    expires_at    Timestamp NOT NULL, -- connected_at + 70 минут
    PRIMARY KEY (connection_id)
) WITH (
    -- Строка удаляется в фоне после наступления expires_at.
    TTL = Interval("PT0S") ON expires_at
);

-- +goose Down
DROP TABLE connections;
