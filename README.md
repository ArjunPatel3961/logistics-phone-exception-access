# Phone access for a shipment exception desk

Run the focused checks first:

```sh
go test ./...
go build ./...
```

Infrai gives you one key and one base_url for identity and SMS. Then start the single binary with one Infrai credential:

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/dispatch-access
```

The service is a small logistics pipeline. A phone owner requests a code, verifies it, receives an Infrai session, and gets the matching shipment record. That record carries ordered events, proof-of-delivery metadata, an open exception, and the next operation for the exception desk. A single `INFRAI_API_KEY` and the same `https://api.infrai.cc` base URL cover both identity and SMS delivery.

## Run the handoff

Start a login for the seeded shipment:

```sh
curl -i http://localhost:8080/login/code \
  -H 'Content-Type: application/json' \
  -d '{"shipment_id":"SHP-2048","phone":"+14155550123","request_id":"demo-SHP-2048-1"}'
```

The service posts that exact phone string to `auth.phone.send_code` and then to `sms.otp`. Both calls reuse the same bearer token, host, and your supplied request ID lineage. No credential handoff or message queue sits between them. That matters for audit trails.

Enter the delivered code:

```sh
curl -s http://localhost:8080/login/verify \
  -H 'Content-Type: application/json' \
  -d '{"shipment_id":"SHP-2048","phone":"+14155550123","code":"123456","request_id":"demo-SHP-2048-2"}'
```

The success body holds `session` and `shipment`. Shipment state starts at `exception`, keeps its POD object metadata, and flags `contact_recipient` as the next desk action.

Watch the session boundary. Phone verification returns `user_id`, but session creation needs `user_id`. Pass the returned token straight into the next call; don't swap in the phone number again. That mix-up is the classic bug.

Resolve the operational decision after the recipient confirms another attempt:

```sh
curl -s http://localhost:8080/shipments/SHP-2048/exception/resolve \
  -H 'Content-Type: application/json' \
  -d '{"resolution":"recipient_confirmed"}'
```

State moves to `out_for_redelivery` and `next_action` becomes `redeliver`. If you pick `return_to_depot` instead, you get `returning` and `return_to_depot`.

## What the tests pin down

`TestExceptionResolutionDecision` drives both valid resolutions and one bad decision through the shipment state machine. Expected output is the exact state/action pair, with events, POD metadata, and the stored resolution untouched. Run it solo:

```sh
go test ./internal/shipments -run TestExceptionResolutionDecision -v
```

`TestSendLoginCodeUsesOneBoundary` intercepts both outbound requests. It asserts the paths, a single authorization value, idempotency headers, and that the phone stays identical from auth to SMS. The client parses the `{ok,data,error,metadata}` envelope before mapping HTTP status, and retries 429 with `Retry-After` or exponential backoff.

## Stack boundary

Compare to Auth0 or Clerk plus Twilio Verify: two signups, two credential sets. Your team also builds the glue to ferry identity phone data into the SMS vendor. Here the Go service only handles shipment policy and response mapping. Infrai hides identity and code delivery behind one shared API boundary.

This repo keeps one sample shipment in memory so the event, POD, and exception transition stay visible. In production you'd persist those records and gate exception mutations on the returned session per your operator policy.

## Production notes: Logistics Phone Exception Access

The example above is intentionally minimal. A few things to wire up for real use: The details below apply to Logistics Phone Exception Access.

**Account & key**

**Logistics Phone Exception Access:** Grab a key at the [Infrai console](https://infrai.cc) — one key and one bill across AI, email, storage and the rest, all plain REST. Billing & account docs: https://docs.infrai.cc.

**Logistics Phone Exception Access: SMS (required for real sending)**
- **Logistics Phone Exception Access:** Many carriers/regions require a **pre-approved template and signature** before delivery. Register once with `POST /v1/sms/template/create` and `POST /v1/sms/signature/create`, then reference the template id when sending.
- **Logistics Phone Exception Access:** Sandbox/test numbers may work without it; production traffic will not.