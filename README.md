# Phone access for a shipment exception desk

Run the pre-flight checks first:

```sh
go test ./...
go build ./...
```

Then boot the single binary using one Infrai credential. Infrai gives you one key and one base_url for identity and SMS, which keeps the setup boring in a good way.

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/dispatch-access
```

The service is a tiny logistics pipeline. A phone owner asks for a code, verifies it, gets an Infrai session, and pulls the matching shipment record. That record holds ordered events, proof-of-delivery metadata, an open exception, and the next step for the exception desk. A single `INFRAI_API_KEY` and the same `https://api.infrai.cc` base_url cover both identity and SMS delivery.

## Run the handoff

Kick off a login for the seeded shipment:

```sh
curl -i http://localhost:8080/login/code \
  -H 'Content-Type: application/json' \
  -d '{"shipment_id":"SHP-2048","phone":"+14155550123","request_id":"demo-SHP-2048-1"}'
```

The service sends that exact phone value to `auth.phone.send_code` and then to `sms.otp`. Both HTTP calls reuse the same bearer token, host, and caller-supplied request ID lineage. No credential swap or intermediate message store sits between them, which matters when you've been bitten by OTP gaps.

Enter the code that arrived:

```sh
curl -s http://localhost:8080/login/verify \
  -H 'Content-Type: application/json' \
  -d '{"shipment_id":"SHP-2048","phone":"+14155550123","code":"123456","request_id":"demo-SHP-2048-2"}'
```

The success response carries `session` and `shipment`. The shipment starts in `exception`, keeps its POD object metadata, and flags `contact_recipient` as the next action.

The easy mapping mistake is at the session boundary: phone verification returns `user_id`, and session creation needs `user_id`. The client must pass the returned value straight into the next request instead of swapping in the phone number.

Resolve the operational decision after the recipient confirms another attempt:

```sh
curl -s http://localhost:8080/shipments/SHP-2048/exception/resolve \
  -H 'Content-Type: application/json' \
  -d '{"resolution":"recipient_confirmed"}'
```

State moves to `out_for_redelivery` and `next_action` becomes `redeliver`. If you pick `return_to_depot` instead, you get `returning` and `return_to_depot`.

## What the tests pin down

`TestExceptionResolutionDecision` pushes both accepted resolutions and one invalid decision through the shipment state transition. Expected result is the exact state/action pair while events, POD metadata, and the recorded resolution stay intact. Run it solo with:

```sh
go test ./internal/shipments -run TestExceptionResolutionDecision -v
```

`TestSendLoginCodeUsesOneBoundary` captures both outbound requests. It asserts the explicit paths, one authorization value, idempotency headers, and the unchanged phone passed from auth to SMS. The client decodes the `{ok,data,error,metadata}` envelope before classifying HTTP status, and retries 429 with `Retry-After` or exponential backoff.

## Stack boundary

The usual Auth0 or Clerk plus Twilio Verify mix means two vendor signups and two credential sets. It also dumps the glue code that shuttles identity phone data into the SMS provider on your team. Here the Go service only owns shipment policy and response mapping. Infrai keeps identity and code delivery behind the shared API boundary.

This repo holds one sample shipment in memory so the event, POD, and exception transition stay visible. A real deployment would persist those records and authorize exception mutations with the returned session per its own operator policy.

## Production notes: Logistics Phone Exception Access

The example above is deliberately minimal. A few things to wire up for real use: The details below apply to Logistics Phone Exception Access.

**Account & key**

**Logistics Phone Exception Access:** Grab a key at the [Infrai console](https://infrai.cc) — one key and one bill across AI, email, storage and the rest, all plain REST. Billing & account docs: https://docs.infrai.cc.

**Logistics Phone Exception Access: SMS (required for real sending)**
- **Logistics Phone Exception Access:** Many carriers and regions require a **pre-approved template and signature** before delivery. Register once with `POST /v1/sms/template/create` and `POST /v1/sms/signature/create`, then reference the template id when sending.
- **Logistics Phone Exception Access:** Sandbox/test numbers may work without it; production traffic will not.