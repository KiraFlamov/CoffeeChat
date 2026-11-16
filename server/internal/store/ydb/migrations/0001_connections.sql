-- Открытые соединения WebSocket.
-- IF NOT EXISTS: если применение оборвалось после создания таблицы, повтор не упадёт.
CREATE TABLE IF NOT EXISTS connections (
    connection_id Utf8 NOT NULL,      -- ID соединения от API Gateway
    connected_at  Timestamp NOT NULL,
    expires_at    Timestamp NOT NULL, -- connected_at + 70 минут
    PRIMARY KEY (connection_id)
) WITH (
    -- Строка удаляется в фоне после наступления expires_at.
    TTL = Interval("PT0S") ON expires_at
);
