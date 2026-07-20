# Frontend transaction WebSocket

## Endpoint

```text
GET /frontend/ws/transaction?transaction_id={transaction_id}&id_tag={id_tag}
```

Production example:

```text
wss://dev-ocpphalapi.transev.site/frontend/ws/transaction?transaction_id=9451203&id_tag=passenger-user-id
```

Both query values must be URL encoded.

The canonical query names are `transaction_id` and `id_tag`. The compatibility
aliases `transactionId` and `idTag` are also accepted.

`transaction_id` must be a positive base-10 integer between `1` and
`2147483647`. The response represents it as a string.

The transaction ID and idTag must match the same persisted HAL transaction.
An unknown transaction or mismatched idTag receives an HTTP `404` before the
WebSocket upgrade. The response deliberately does not disclose which value
failed to match.

## Behavior

The server sends a complete persisted transaction snapshot:

1. immediately after the WebSocket connects;
2. immediately after a MeterValues update is persisted;
3. immediately after charger-originated StopTransaction is persisted;
4. when `max_kwh` or the automatic-limit-stop state changes;
5. every 30 seconds as a durable-store resynchronization snapshot.

The periodic snapshot is not a connection timeout. The server does not impose a
maximum socket lifetime or idle timeout.

All event notifications cause the handler to re-read the transaction store.
The notification itself is never treated as transaction truth.

## Running response

```json
{
  "event": "transaction_snapshot",
  "status": "RUNNING",
  "transaction": {
    "id": 42,
    "uuiddb": "6cc5f552-164e-4c1d-980f-974961485a30",
    "charger_id": "CP-001",
    "connector_id": 1,
    "meter_start": 1000,
    "meter_stop": 1750,
    "total_consumption": 0.75,
    "start_time": "2026-07-20T10:00:00Z",
    "stop_time": null,
    "id_tag": "passenger-user-id",
    "transaction_id": "9451203",
    "is_single_session": false,
    "max_kwh": 7.5,
    "limit_stop_requested": false
  },
  "observed_at": "2026-07-20T10:02:00Z"
}
```

`meter_start` and `meter_stop` are cumulative charger meter readings in Wh.
`total_consumption` and `max_kwh` are in kWh.

## Completed response

After the charger sends StopTransaction, the same socket receives:

```json
{
  "event": "transaction_snapshot",
  "status": "COMPLETED",
  "transaction": {
    "transaction_id": "9451203",
    "id_tag": "passenger-user-id",
    "meter_start": 1000,
    "meter_stop": 3500,
    "total_consumption": 2.5,
    "stop_time": "2026-07-20T10:30:00Z"
  },
  "observed_at": "2026-07-20T10:30:00Z"
}
```

The actual frame contains every transaction field shown in the running example;
the shortened example highlights the completion fields.

`COMPLETED` means the Go HAL has persisted charger-originated StopTransaction.
CMS billing, wallet deduction, and charging-session history are processed
through the durable completion callback and may become visible shortly after
this frame.
