# Spice harvester

[![Telegram chat][telegram-svg]][telegram-url]

**!IMPORTANT!** The service is in the beta testing stage.

The service facilitates payment tracking within the TON blockchain, offering a user-friendly API for generating invoices 
in multiple currencies (TON, Jettons). It also features a mechanism to deliver purchase data for client-side display.

- [How it works](#How-it-works)
- [Provided API](#Provided-API)
- [Invoice layout](#Invoice-layout)
- [Metadata layout](#Metadata-layout)
- [Invoice state diagram](#Invoice-state-diagram)
- [Notifications](#Notifications)
- [Payment methods](#Payment-methods)
- [Deploy](#Deploy)

## Features

* Multi-currency support (TON, Jettons)
* Open-source solution
* Self-hosted solution
* Notification channel for invoice status updates
* Metadata delivery mechanism for displaying purchase details on client apps
* Payment through a payment link (seamless QR code integration)
* Fund security (enables payment monitoring without wallet access)

## Prerequisites

* Initialized (at blockchain) wallet smart contract (and Jetton wallet smart contracts) for receiving funds
* Access to a stable lite server
* Jettons must comply with standard [TEP-74](https://github.com/ton-blockchain/TEPs/blob/master/text/0074-jettons-standard.md)
* Docker (for deploy via docker)

## How-it-works

### Basic operation scenario

The basic scenario provides core invoice handling functions.
1. The merchant creates an invoice by entering the required details.
2. The merchant provides the buyer with the payment link from the invoice.
3. The buyer completes the payment through the provided link.
4. The merchant monitors the invoice status via REST API or notification channel. Once the status updates to "paid," the order is confirmed as paid.

To deploy the service for the basic scenario, use [Deploy without discoverable metadata](#Deploy-without-discoverable-metadata).

![Image](/docs/basic_interaction_diagram.drawio.svg)

### Advanced operation scenario

In addition to the basic scenario functions, the harvester API also provides the ability to supply metadata of paid invoices for indexing and displaying purchase history in wallets and applications.
Metadata is encrypted and can only be decrypted by the party that paid the invoice, meaning this data is not public.

The use of this scenario does not require additional integration from the merchant. 
During deployment, a proxy server will be launched through which wallet application will be able to obtain encrypted metadata.
All private API endpoints are protected by a token, and data from them is not accessible to third-party applications.

To deploy the service for the advanced scenario, use [Deploy with discoverable metadata](#Deploy-with-discoverable-metadata).

![Image](/docs/advanced_interaction_diagram.drawio.svg)

## Provided API

REST API is described in file [swagger.yaml](/api/swagger.yaml).

## Invoice layout

In the REST API and notifications, invoices are presented in the following structure:

```json
{
  "id": "03cfc582-b1c3-410a-a9a7-1f3afe326b3b",
  "status": "waiting",
  "amount": "100000000",
  "currency": "TON",
  "pay_to_address": "0:81...88",
  "payment_links": {
    "universal": "ton://transfer/UQ...qr?amount=100000000&bin=te...pP&exp=1744066284",
    "tonkeeper": "https://app.tonkeeper.com/transfer/UQ...qr?amount=100000000&bin=te...pP&exp=1744066284"
  },
  "created_at": 1744063284,
  "expire_at": 1744066284,
  "updated_at": 1744063284,
  "private_info": {
      "order_number": 123
  },
  "metadata": {
    "goods": [
      {
        "name": "Latte 300ml"
      }
    ], 
    "mcc_code": 5462,
    "merchant_name": "Coffee shop",
    "merchant_url": "https://coffee.com/"
  },
  "overpayment": "0"
}
```

* `id` - a unique invoice identifier, publicly available and recorded on the blockchain upon payment. Essential for tracking invoice statuses through the API and accessing metadata.
* `status` - the invoice's current status. Refer to the [Invoice state diagram](#Invoice-state-diagram).
* `amount` - the total amount in the smallest indivisible units.
* `currency` - the currency ticker; see [Currency tickers](#Currency-tickers).
* `pay_to_address` - the recipient's wallet address (in raw format).
* `payment_links` - a set of payment links in various formats.
* `created_at` - the timestamp of invoice creation in Unix time.
* `expire_at` - the invoice's expiration timestamp (Unix time). Payments received after this time will not mark the invoice as paid.
* `updated_at` - the timestamp of the last invoice change in Unix time.
* `private_info` - non-public, arbitrary JSON data for API integration.
* `metadata` - purchase information (format detailed in the [Metadata layout](#Metadata-layout)) intended for buyer display.
* `overpayment` - information on any overpayments for the invoice, in the same units and currency as the amount. If a lesser amount than required is received, it will be recorded as an overpayment.

### Currency tickers
For TON, the ticker `TON` is always used.
For other currencies, tickers are set by the administrator when configuring variable `JETTONS` ([Environment variables](#ENV-variables)).
For payment processing, the Jetton address is always used. Tickers are only used for display in the invoice.

## Metadata layout

For consistent data display on the buyer's side, the schema is explicitly defined but may be extended in the future.

```json
{
  "goods": [
    {
      "name": "Latte 300ml"
    }
  ],
  "mcc_code": 5462,
  "merchant_name": "Coffee shop",
  "merchant_url": "https://coffee.com/"
}
```

1. `merchant_name` - merchant's name (mandatory field)
2. `merchant_url` - link to the merchant's website (optional)
3. `mcc_code` - merchant category code, codes are specified by the ISO 18245 standard. [Wikipedia](https://en.wikipedia.org/wiki/Merchant_category_code)
4. `goods` - list of goods (mandatory field, though it can be empty)
    1. `name` - product name (mandatory field)

## Invoice state diagram

![Image](/docs/invoice_state_diagram.drawio.svg)

## Notifications

You can receive notifications about invoice status changes via webhooks if you specify an `WEBHOOK_ENDPOINT` when deploying the service. 
Any transition of an invoice from one state to another will trigger a notification with the [Invoice layout](#Invoice-layout) json of the invoice in its new state.

## Payment methods

The primary method for paying an invoice is a payment link. You can either provide the link directly to the payer or 
encode it into a QR code and display the code. When using the payment link, the necessary information for metadata sharing will be attached. 
This data will be encoded in binary format within the message.

There is an alternative payment method. For this, you need to attach the invoice ID as a text comment during payment. 
In this case, the necessary information for metadata detection will not be attached. Metadata and payment history will be unavailable.
This method is not recommended by default and should only be used as a backup if the first method is unavailable.

## Deploy

To deploy the service, you will need to fill in the [Environment variables](#ENV-variables).  
You can specify them directly in the file [docker-compose.yml](/docker-compose.yml) or create an `.env` file in the same directory and specify these variables there ([.env file example](#env-file-example)).

### ENV variables

| ENV variable        | Type   | Mandatory | Description                                                                                                                                                                                                                                                                                                                                       |
|---------------------|--------|-----------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `POSTGRES_USER`     | string | yes       | database username                                                                                                                                                                                                                                                                                                                                 |
| `POSTGRES_PASSWORD` | string | yes       | database password                                                                                                                                                                                                                                                                                                                                 |
| `POSTGRES_URI`      | string | yes       | URI for DB connection, example: <br/>`postgresql://POSTGRES_USER:POSTGRES_PASSWORD@harvester_postgres/harvester?sslmode=disable` <br/>It may differ when using the database outside of Docker                                                                                                                                                     |
| `TOKEN`             | string | yes       | bearer token for accessing the private part of the API                                                                                                                                                                                                                                                                                            |
| `RECIPIENT`         | string | yes       | wallet address for receiving payments in [raw or user-friendly form](https://docs.ton.org/v3/concepts/dive-into-ton/ton-blockchain/smart-contract-addresses/#address-formats)                                                                                                                                                                     |
| `LITE_SERVERS`      | string | no        | list of liteservers in the form of `<IP1>:<PORT1>:<KEY1>,<IP2>:<PORT2>:<KEY2>` <br/>example: `5.9.10.15:48014:3XO67K/qi+gu3T9v8G2hx1yNmWZhccL3O7SoosFo8G0=` <br/>The list is automatically taken from [global-config.json](https://ton.org/global-config.json) if the variable is not set                                                         |
| `LOG_LEVEL`         | string | no        | possible options: `DEBUG`, `INFO`, `WARN`, `ERROR`. Default: `INFO`                                                                                                                                                                                                                                                                               |
| `JETTONS`           | string | no        | list of tokens for receiving payments: `ticker1 decimals1 address1, ticker2 decimals2 address2` <br/>example: `USDT 6 EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs,NOT 9 EQAvlWFDxGF2lXm67y4yzC17wYKD9A0guwPkMs1gOsM__NOT`                                                                                                                    |
| `WEBHOOK_ENDPOINT`  | string | no        | endpoint for sending webhooks, example: `https://your-server.com/webhook`                                                                                                                                                                                                                                                                         |
| `PAYMENT_PREFIXES`  | string | no        | list of prefixes for generating payment links: `name_1 prefix1,name_1 prefix2` <br/>The `name` is used as a key in the list of payment links (see [Invoice layout](#Invoice-layout)) <br/>The prefixes `ton://` with `universal` name and `https://app.tonkeeper.com/` with `tonkeeper` name are supported by default and do not need to be added |
| `KEY`               | string | no        | 32 bytes written in hex format (see [Key generation](#Key-generation))                                                                                                                                                                                                                                                                            |
| `EXTERNAL_IP`       | string | no        | external IP of the TON proxy. It can be determined automatically if not specified                                                                                                                                                                                                                                                                 |

### .env file example

**!IMPORTANT!** Substitute your own values before use.

```dosini
# harvester-postgres
# mandatory parameters:
HARVESTER_POSTGRES_USER="<postgres_user>"
HARVESTER_POSTGRES_PASSWORD="<postgres_password>"

# harvester-api
# mandatory parameters:
HARVESTER_POSTGRES_URI="postgres://<postgres_user>:<postgres_password>@harvester_postgres/harvester?sslmode=disable"
HARVESTER_API_TOKEN="<api_token>"
HARVESTER_RECIPIENT="<wallet_address_for_receiving_payments>"
# optional parameters:
HARVESTER_LITE_SERVERS="<IP>:<PORT>:<KEY>,5.9.10.15:48014:3XO67K/qi+gu3T9v8G2hx1yNmWZhccL3O7SoosFo8G0="
HARVESTER_KEY="<32_random_bytes_in_hex_representation>"
HARVESTER_JETTONS="<ticker1> <decimals1> <address1>,<ticker2> <decimals2> <address2>,USDT 6 EQCxE6mUtQJKFnGfaROTKOt1lZbDiiX1kCixRv7Nw2Id_sDs"
HARVESTER_WEBHOOK_ENDPOINT="https://your-server.com/webhook"

# harvester-reverse-proxy
# optional parameters:
TON_PROXY_EXTERNAL_IP="1.2.3.4"
```

### Deploy without discoverable metadata

All mandatory ENV variables must be set in `docker-compose.yml` or `.env` file.

In some cases `sudo` may be required for `docker` command.
```bash
docker compose -f docker-compose.yml up -d harvester-api
```

### Deploy with discoverable metadata

All mandatory ENV variables must be set in `docker-compose.yml` or `.env` file.
In addition to the mandatory variables, a key ([Key generation](#Key-generation)) must be generated and the `KEY` variable must be set.

In some cases `sudo` may be required for `docker` command.
```bash
docker compose -f docker-compose.yml up -d harvester-reverse-proxy
docker compose -f docker-compose.yml up -d harvester-api
```

**!IMPORTANT!** Similarly, to operate the proxy, you need to open the port (`9306/udp` by default).

### Update
In some cases `sudo` may be required for `docker` command.

If you need to update the `harvester-api`, you need to build a new Docker image from the new version of the source code with the command:
```bash
docker build -t spice-harvester-api:latest --target spice-harvester-api .
```
Then you need to remove the old service:
```bash
docker stop harvester_api
docker rm harvester_api
```

If you need to update the `harvester-reverse-proxy`, you need to build a new Docker image from the new version of the source code with the command:
```bash
docker build -t ton-reverse-proxy:latest --target ton-reverse-proxy .
```
Then you need to remove the old service:
```bash
docker stop harvester_reverse_proxy
docker rm harvester_reverse_proxy
```

After which deploy the new service as in [Deploy with discoverable metadata](#Deploy-with-discoverable-metadata) or [Deploy without discoverable metadata](#Deploy-without-discoverable-metadata).

### Key generation

The key is used for encrypting metadata and generating the server's ADNL address. This key is not used for interacting with the blockchain or performing any transactions.

**!IMPORTANT!** Make sure to save a copy of the key in a secure place after generation.

The key consists of 32 random bytes written in hex format.  
Any cryptographically secure tool can be used to generate the key.

Example of key generation in Linux:
```bash
openssl rand -hex 32
```

<!-- Badges -->
[telegram-url]: https://t.me/txsociety
[telegram-svg]: https://img.shields.io/badge/telegram-chat-blue?color=blue&logo=telegram&logoColor=white