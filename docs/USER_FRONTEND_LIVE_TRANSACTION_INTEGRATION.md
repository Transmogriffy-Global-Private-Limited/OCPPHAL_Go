# Passenger frontend live-transaction integration

## Scope

This is the current, verified passenger-frontend contract spanning:

- CMS start and current-session discovery;
- the OCPPHAL Go live transaction WebSocket;
- CMS exact-transaction stop;
- charger-originated completion;
- CMS completed charging-session history.

The exact OCPP transaction ID is the join key across the systems:

```text
CMS ChargerTransaction.transactionid
    =
OCPPHAL transactions.transaction_id
    =
CMS Charingsessions.sessionid
```

Represent it as a string in frontend code. Never substitute the CMS row `uid`,
the Go row `uuiddb`, charger ID, connector ID, or newest array entry.

## Frontend base URLs

Use the deployed values already provided to the frontend:

```text
CMS_BASE_URL = VITE_BK_ROOT_URI
HAL_WS_BASE_URL = VITE_FE_WS_URI
```

For the current development OCPPHAL deployment:

```text
HAL_WS_BASE_URL=wss://dev-ocpphalapi.transev.site
```

Remove a trailing slash before appending paths.

## End-to-end sequence

1. Query the CMS current-session endpoint before offering a new start.
2. Send the start request to CMS.
3. A start `200` means RemoteStartTransaction was accepted, not that an OCPP
   transaction exists yet.
4. Poll the CMS current-session endpoint until CMS returns the charger-created
   transaction ID.
5. Open the HAL transaction WebSocket with that exact `transactionid` and the
   returned transaction `userid` as `id_tag`.
6. Render live meter and consumption fields from WebSocket snapshots.
7. To stop, send the exact transaction ID, charger ID, and user ID to CMS.
8. Keep the WebSocket open. A stop HTTP acknowledgement is not completion.
9. Treat a WebSocket `COMPLETED` snapshot as charger-originated HAL completion.
10. Fetch CMS charging-session history until the matching `sessionid` appears.

Do not call the HAL `/api/start_transaction` or `/api/stop_transaction` routes
from the passenger frontend. CMS owns wallet checks, lifecycle state, retry
logic, and transaction ownership checks.

## 1. Discover a current CMS transaction

```http
POST {CMS_BASE_URL}/users/getongoingtransaction
Content-Type: application/json
Authorization: Bearer <CMS JWT>
```

```json
{
  "userid": "passenger-user-id"
}
```

An optional `chargerid` may narrow the query.

Relevant `200` response:

```json
{
  "ongoing": true,
  "ambiguous": false,
  "transaction": {
    "uid": "cms-row-uuid",
    "chargerid": "CP-001",
    "userid": "passenger-user-id",
    "transactionid": "9451203",
    "connectorid": "1",
    "max_kwh": "7.50",
    "status": "ACTIVE"
  },
  "ongoing_transactions": [
    {
      "chargerid": "CP-001",
      "userid": "passenger-user-id",
      "transactionid": "9451203",
      "connectorid": "1",
      "status": "ACTIVE"
    }
  ]
}
```

`404` with `ongoing: false` is the normal no-current-session result.

If `ongoing_transactions` contains multiple entries, render each entry keyed by
`transactionid`. Never silently select the newest entry for a stop request.

## 2. Request a start through CMS

```http
POST {CMS_BASE_URL}/users/chargerstart
Content-Type: application/json
Authorization: Bearer <CMS JWT>
```

```json
{
  "chargerid": "CP-001",
  "userid": "passenger-user-id",
  "useraccept": true,
  "connectorid": "1"
}
```

After a successful response, poll `/users/getongoingtransaction`. The start
response cannot reliably contain `transactionid`, because only the later
charger-originated OCPP StartTransaction creates it.

Before retrying any failed or timed-out start, query the current-session
endpoint again.

## 3. Open the live transaction WebSocket

Canonical endpoint:

```text
{HAL_WS_BASE_URL}/frontend/ws/transaction?transaction_id={transactionid}&id_tag={userid}
```

Example:

```text
wss://dev-ocpphalapi.transev.site/frontend/ws/transaction?transaction_id=9451203&id_tag=passenger-user-id
```

Construct it with URL encoding:

```ts
function openTransactionSocket(input: {
  wsBaseUrl: string;
  transactionid: string;
  userid: string;
}): WebSocket {
  const base = input.wsBaseUrl.replace(/\/+$/, "");
  const query = new URLSearchParams({
    transaction_id: input.transactionid,
    id_tag: input.userid,
  });

  return new WebSocket(`${base}/frontend/ws/transaction?${query.toString()}`);
}
```

The endpoint accepts canonical `transaction_id` and `id_tag`; camel-case
`transactionId` and `idTag` are compatibility aliases.

`transaction_id` must be an unsigned base-10 integer from `1` through
`2147483647`. The exact `(transaction_id, id_tag)` tuple must match a persisted
HAL transaction.

Handshake errors:

| HTTP | Meaning |
| --- | --- |
| `400` | Missing or invalid transaction ID, or missing idTag. |
| `404` | The exact transaction/idTag tuple does not exist. |
| `503` | Transaction streaming is unavailable in the HAL process. |

The current endpoint does not accept a CMS JWT or HAL API key during the
WebSocket handshake. Tuple matching is required, but it is not a replacement
for future authenticated WebSocket access.

## 4. Process live snapshots

Every message has one shape:

```ts
type HalTransactionStatus = "RUNNING" | "COMPLETED";

interface HalTransactionRow {
  id: number;
  uuiddb: string;
  charger_id: string;
  connector_id: number;
  meter_start: number;
  meter_stop: number | null;
  total_consumption: number | null;
  start_time: string;
  stop_time: string | null;
  id_tag: string;
  transaction_id: string;
  is_single_session: boolean;
  max_kwh: number | null;
  limit_stop_requested: boolean;
}

interface HalTransactionSnapshot {
  event: "transaction_snapshot";
  status: HalTransactionStatus;
  transaction: HalTransactionRow;
  observed_at: string;
}
```

Units:

- `meter_start` and `meter_stop`: cumulative charger meter readings in Wh;
- `total_consumption`: session consumption in kWh;
- `max_kwh`: CMS-calculated automatic-stop limit in kWh.

The HAL sends a snapshot:

- immediately after connection;
- immediately after a MeterValues update is persisted;
- immediately after StopTransaction is persisted;
- after max-kWh or automatic-limit-stop state changes;
- every 30 seconds as a database resynchronization snapshot.

The 30-second snapshot is not a connection timeout. The HAL sets no maximum
frontend socket lifetime.

Reference message handling:

```ts
socket.onmessage = (event) => {
  const snapshot = JSON.parse(event.data) as HalTransactionSnapshot;

  if (
    snapshot.event !== "transaction_snapshot" ||
    snapshot.transaction.transaction_id !== expectedTransactionId
  ) {
    return;
  }

  renderConsumption(snapshot.transaction.total_consumption ?? 0);

  if (snapshot.status === "COMPLETED") {
    renderCompleted(snapshot.transaction);
    void refreshMatchingCmsHistory(snapshot.transaction.transaction_id);
  }
};
```

On disconnect, reconnect with the same transaction ID and idTag. The first
message will contain the latest persisted row, including a completed row.

## 5. Request an exact stop through CMS

```http
POST {CMS_BASE_URL}/users/chargerstop
Content-Type: application/json
Authorization: Bearer <CMS JWT>
```

```json
{
  "chargerid": "CP-001",
  "userid": "passenger-user-id",
  "transactionid": "9451203"
}
```

Use fields from the same selected CMS transaction. Do not recompute or replace
the ID.

A successful response such as:

```json
{
  "message": "Charger stop requested",
  "status": "Accepted",
  "transactionid": "9451203"
}
```

means only that RemoteStopTransaction was accepted. Keep rendering
`STOP_PENDING` until the HAL WebSocket reports `COMPLETED`.

If CMS returns `retry_scheduled: true`, keep the session visible and pending;
CMS owns the durable remote-stop retry.

## 6. Completed charging-session history

The current CMS history endpoint is:

```http
POST {CMS_BASE_URL}/users/chargingsessionbyuserid
Content-Type: application/json
```

```json
{
  "userid": "passenger-user-id"
}
```

Successful response:

```json
{
  "message": "All of the data",
  "data": [
    {
      "id": "database-row-id",
      "uid": "cms-session-uuid",
      "sessionid": "9451203",
      "chargerid": "CP-001",
      "startime": "2026-07-20T10:00:00Z",
      "stoptime": "2026-07-20T10:30:00Z",
      "meterstart": "1000",
      "meterstop": "3500",
      "consumedkwh": "2.5",
      "totalcost": "45.00",
      "createdAt": "2026-07-20T10:30:01.000Z",
      "updatedAt": "2026-07-20T10:30:01.000Z",
      "userid": "passenger-user-id",
      "associatedadminid": "admin-id"
    }
  ]
}
```

Important current behavior:

- `sessionid` is the exact OCPP transaction ID and is the field to match;
- an empty history is `200` with `data: []`, not `404`;
- the endpoint returns every matching row;
- it has no pagination, explicit ordering, date filter, or transaction-ID
  filter;
- database return order is not a frontend contract;
- the controller currently trusts body `userid` and does not enforce JWT
  ownership itself.

The frontend may send its bearer token for forward compatibility, but the
current history controller does not validate it. This endpoint should receive
CMS ownership enforcement before being treated as privacy-safe.

Never use `data[0]` as the completed transaction. Match exactly:

```ts
interface CmsChargingSession {
  sessionid: string;
  chargerid: string | null;
  userid: string | null;
  startime: string | null;
  stoptime: string | null;
  meterstart: string | null;
  meterstop: string | null;
  consumedkwh: string | null;
  totalcost: string | null;
}

async function fetchMatchingHistory(input: {
  cmsBaseUrl: string;
  userid: string;
  transactionid: string;
  token?: string;
}): Promise<CmsChargingSession | null> {
  const response = await fetch(
    `${input.cmsBaseUrl.replace(/\/+$/, "")}/users/chargingsessionbyuserid`,
    {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        ...(input.token
          ? { Authorization: `Bearer ${input.token}` }
          : {}),
      },
      body: JSON.stringify({ userid: input.userid }),
    },
  );

  if (!response.ok) {
    throw new Error(`Charging history request failed: ${response.status}`);
  }

  const body = await response.json();
  const sessions = Array.isArray(body.data) ? body.data : [];

  return (
    sessions.find(
      (session: CmsChargingSession) =>
        String(session.sessionid) === input.transactionid,
    ) ?? null
  );
}
```

## Completion/history timing

The HAL publishes `COMPLETED` immediately after it persists the
charger-originated StopTransaction. CMS history is created afterward through
the durable completion callback.

Therefore, the first history request after a completed WebSocket frame may
legitimately return no matching row. Retry only the history read with bounded
backoff, for example:

```text
immediately, 2s, 4s, 8s, 15s, 30s
```

Do not resend stop because history or a PDF bill is temporarily absent.

## Recovery rules

- Page reload while running: call CMS current-session discovery, then reconnect
  the HAL socket using the returned exact transaction.
- WebSocket interruption: preserve the last snapshot and reconnect with the same
  tuple.
- App resumes after completion: current-session discovery may return `404`;
  fetch CMS history and match the stored transaction ID if available.
- Stop request times out: query current CMS state and keep the same live socket;
  never start a replacement session without checking current state.
- History read fails: preserve the HAL completed state and retry history; do not
  regress the UI to running.

## Frontend acceptance checklist

- [ ] Transaction IDs are stored and compared as strings.
- [ ] Start is sent only through CMS.
- [ ] Current CMS state is checked before every start attempt.
- [ ] The WebSocket opens only after CMS supplies the exact transaction ID.
- [ ] WebSocket `id_tag` comes from the same transaction's `userid`.
- [ ] Live energy comes from `total_consumption`, not raw `meter_stop`.
- [ ] Stop contains the exact transaction ID, charger ID, and user ID.
- [ ] Stop acknowledgement is displayed as pending, not completed.
- [ ] Completion is accepted only from persisted HAL StopTransaction truth.
- [ ] CMS history is matched by `sessionid === transaction_id`.
- [ ] History array order is never used to identify a session.
- [ ] Temporary history/bill delay does not cause another stop or start.
- [ ] WebSocket reconnect restores both running and completed transactions.
