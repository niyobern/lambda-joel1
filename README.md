# Paypack Cash-In Lambda

This project converts the original Python payment utility into a Go-based AWS Lambda function that initiates a Paypack **cash-in** whenever a user subscribes in your web app. After the cash-in request, the Lambda polls `find_transaction` every few seconds for up to **5 minutes** (configurable) until the transaction is confirmed. Once the transaction resolves (success or timeout), the Lambda posts the result to your Next.js API route so the app can update the subscription state.

## Architecture overview

- **Handler entrypoint**: `cmd/lambda/main.go` wires the AWS Lambda runtime to the `internal/handler` package.
- **Payment client**: `internal/paypack` wraps the Paypack REST API (`authorize`, `cashin`, `find_transaction`), handling bearer tokens, retries, and JSON models.
- **Processor flow**:
  1. Validate incoming subscription event payload.
  2. Call `cashin` with the supplied number and amount.
  3. Poll `find_transaction` until a `Transaction` is returned or 5 minutes elapse.
  4. Build a `SubscriptionResponse` reflecting **success** (transaction found) or **failure** (timeout after 5 minutes, per the mobile-money SLA).
  5. POST the response body to your Next.js callback URL for subscription updates, then return it to the original caller.

## Environment variables

Set these variables in Lambda (or locally) before running the binary:

| Variable | Required | Description |
| --- | --- | --- |
| `PAYPACK_APP_ID` | ✅ | Paypack application ID (maps to `app_id` in the original Python file). |
| `PAYPACK_APP_SECRET` | ✅ | Paypack application secret (`app_secret`). |
| `PAYPACK_BASE_URL` | ⛔️ | Optional override (defaults to `https://payments.paypack.rw`). |
| `SUBSCRIPTION_CALLBACK_URL` | ✅ | HTTPS endpoint in your Next.js app that should receive the transaction outcome. Example: `https://app.example.com/api/subscription/confirm`. |
| `SUBSCRIPTION_CALLBACK_SECRET` | ⛔️ | Shared secret included as `X-Callback-Secret` to authenticate the Lambda → Next.js call. Leave empty to skip the header. |

Secrets should be stored in AWS Secrets Manager or Parameter Store and provided to Lambda via environment variables at deploy time.

## Event contract

The Lambda expects a JSON payload shaped as follows (additional keys are ignored):

```json
{
  "number": "+250780000000",
  "amount": 5000,
  "client": "+250780000000",
  "metadata": {
    "plan": "pro",
    "userId": "12345"
  }
}
```

- `number` (**required**): MSISDN that should be charged via cash-in.
- `amount` (**required**): Amount to debit (integer/float). Must be positive.
- `client`, `metadata` (**optional**): forwarded for auditing and logging.

### Lambda response

```json
{
  "ref": "dbed4dbb-f1bd-433d-ba57-e383c5faa96b",
  "status": "success",
  "found": true,
  "transaction": { "ref": "...", "amount": 5000, "kind": "cashin", ... }
}
```

If the transaction is still pending after 5 minutes, the response contains `"found": false`, `"status": "failed"`, and `"message": "transaction not confirmed within 5 minutes"`. This mirrors the mobile-money hard limit for pending transactions.

### Callback contract

Immediately after computing the `SubscriptionResponse`, the Lambda performs an HTTP `POST` to `SUBSCRIPTION_CALLBACK_URL` with that JSON body:

```http
POST /api/subscription/confirm HTTP/1.1
Content-Type: application/json
X-Callback-Secret: <SUBSCRIPTION_CALLBACK_SECRET>

{
  "ref": "...",
  "status": "success|failed",
  "found": true,
  "message": "...",
  "transaction": { ... },
  "request": { "number": "+250...", "amount": 5000, "metadata": { ... } }
}
```

Your Next.js API route should verify the optional `X-Callback-Secret`, update the subscription record, and return `200 OK`. Any non-2xx response or network failure is logged but does **not** block the Lambda response to the original caller.

## Local testing

```bash
export PAYPACK_APP_ID=your-app-id
export PAYPACK_APP_SECRET=your-app-secret
export SUBSCRIPTION_CALLBACK_URL=http://localhost:3000/api/subscription/confirm
export SUBSCRIPTION_CALLBACK_SECRET=dev-shared-secret

# Run unit tests
cd /Users/berniyo/code
go test ./...

# Optionally run the handler locally using the AWS SAM CLI or aws-lambda-go `main` binary
GOOS=linux GOARCH=amd64 go build -o bootstrap ./cmd/lambda
```

When testing locally without invoking real Paypack endpoints, replace `paypack.Client` with a stub that satisfies the `handler.PaymentClient` interface (see `internal/handler/subscription_test.go`).

## Deployment tips

- Build a Linux binary (`GOOS=linux GOARCH=amd64`) and package it as a ZIP for Lambda, or use AWS SAM/Serverless Framework.
- Configure the Lambda timeout to **at least 6 minutes** to accommodate the 5-minute polling window and network overhead.
- Attach IAM permissions to fetch the Paypack secrets if they reside in AWS Secrets Manager/SSM.
- Use CloudWatch Logs to observe the polling and callback lifecycle (`paypack-lambda` logger prefix).

### Deploying from scratch (API Gateway + Lambda)

1. **Create the Lambda function**
  - Runtime: Go 1.x.
  - Timeout: ≥ 360 seconds.
  - Environment variables: `PAYPACK_*`, `SUBSCRIPTION_CALLBACK_*` (pull secrets from Secrets Manager if possible).
  - Upload the zipped `bootstrap` binary (`GOOS=linux GOARCH=amd64 go build -o bootstrap ./cmd/lambda`).
2. **Expose an HTTPS endpoint**
  - Create an HTTP API (API Gateway v2) with a route such as `POST /subscribe`.
  - Integrate the route with the Lambda (Lambda proxy integration).
  - Deploy the API; note the invoke URL.
3. **Secure the endpoint**
  - Configure IAM, JWT authorizers, or API keys depending on your Next.js deployment needs.
  - Store the API URL in your Next.js environment and invoke it when a user requests a subscription.
4. **Handle callbacks in Next.js**
  - Implement `/api/subscription/confirm` to accept the payload above, check `X-Callback-Secret`, and persist subscription state.
5. **Observe & iterate**
  - Use CloudWatch Logs for both the Lambda and API Gateway to monitor real payment attempts.
  - Add alarms on Lambda errors or callback failures if you need operational visibility.

## Extensibility

- Tune `handler.WithPollInterval` and `handler.WithTimeout` if certain providers require faster/slower polling.
- Extend `SubscriptionEvent` and `SubscriptionResponse` structs to propagate additional metadata to downstream systems.
- Add more Paypack endpoints to `internal/paypack/client.go` following the existing pattern.
