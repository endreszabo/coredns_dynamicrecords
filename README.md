# DynamicRecords - CoreDNS Plugin

A CoreDNS plugin that serves DNS records from an in-memory buffer populated via an mTLS-protected HTTPS API or a FrameStreams streaming channel. Records have configurable expiry times and are automatically cleaned up when expired.

## Features

- **In-memory buffer**: Fast DNS record storage with automatic expiry
- **mTLS authentication**: Secure HTTPS API with mutual TLS authentication
- **FrameStreams ingestion**: High-throughput binary streaming channel over TLS (ALPN `fstrm`)
- **RFC1035 format**: Accept records in standard zone file format
- **Consistent ACK/NACK responses**: Both HTTPS and FrameStreams return uniform `{"ok": bool, ...}` JSON
- **Middleware behavior**: Augments downstream plugin responses
- **Smart response handling**: Only adds records for NODATA (rcode 0 with no answers) or NXDOMAIN responses
- **Transparent passthrough**: Preserves other response codes from downstream plugins

## How It Works

The plugin acts as a middleware in the CoreDNS chain:

1. DNS query arrives
2. Query is passed to downstream plugins (e.g., forward, file, etc.)
3. Plugin captures the downstream response using a `nonWriter`
4. If the response is NODATA or NXDOMAIN, plugin checks the in-memory buffer for matching records
5. If matching records exist and haven't expired, they're appended to the ANSWER section and rcode is set to 0
6. If no match or other rcodes, the original response is passed through unchanged

## Configuration

Add to your `Corefile`:

```
example.com {
    dynamicrecords {
        http_addr  :8053
        fstrm_addr :8054
        cert /path/to/server.crt
        key  /path/to/server.key
        ca   /path/to/ca.crt
        default_ttl 300
    }
    forward . 8.8.8.8
}
```

### Configuration Options

- **http_addr**: Address for the HTTPS API server (default: `:8053`)
- **fstrm_addr**: Address for the FrameStreams TLS listener (optional; omit to disable)
- **cert**: Path to server TLS certificate (required)
- **key**: Path to server TLS private key (required)
- **ca**: Path to CA certificate for client verification (required)
- **default_ttl**: Default TTL in seconds if not specified in API request (default: `300`)

### Multiple Domains (Shared HTTP Server)

The plugin supports multiple server blocks (domains) while using a **single shared HTTP server**. All plugin instances share the same in-memory buffer and HTTP listener:

```
example.com {
    dynamicrecords {
        http_addr  :8053
        fstrm_addr :8054
        cert /path/to/server.crt
        key  /path/to/server.key
        ca   /path/to/ca.crt
        default_ttl 300
    }
    forward . 8.8.8.8
}

example.org {
    dynamicrecords {
        http_addr  :8053
        fstrm_addr :8054
        cert /path/to/server.crt
        key  /path/to/server.key
        ca   /path/to/ca.crt
        default_ttl 300
    }
    forward . 8.8.8.8
}
```

**Key Points:**
- All instances **must use the same `http_addr`** (e.g., `:8053`)
- All instances **must use the same certificate files** (cert, key, ca)
- The shared server starts when the first plugin instance loads
- The shared server stops when the last plugin instance shuts down
- All domains share the same in-memory buffer
- Records can be served across any domain (e.g., add a record for `test.example.com` and it will be served when querying the `example.com` server block)

## API Response Format

All endpoints — both HTTPS and FrameStreams — return a uniform JSON envelope:

```json
{"ok": true,  "message": "Added 2 records for test.example.com./A"}
{"ok": false, "error":   "invalid record format: ..."}
```

| Field     | Type   | Present when |
|-----------|--------|--------------|
| `ok`      | bool   | always       |
| `message` | string | success only |
| `error`   | string | failure only |

All HTTPS error responses use `Content-Type: application/json` (not `text/plain`).

## HTTPS API Endpoints

### Add Records

**POST /records**

Add or update an RRset in the buffer.

```bash
curl -X POST https://localhost:8053/records \
  --cert client.crt \
  --key client.key \
  --cacert ca.crt \
  -H "Content-Type: application/json" \
  -d '{
    "ttl": 300,
    "expiry": 1735689600,
    "records": [
      "test.example.com. 300 IN A 192.0.2.1",
      "test.example.com. 300 IN A 192.0.2.2"
    ]
  }'
```

**Request Fields:**

- `records` (required): Array of records in RFC1035 zone file format. QName and QType are automatically extracted from these records. All records must have the same qname and qtype.
- `ttl` (optional): TTL override in seconds (uses TTL from records or default_ttl if not provided)
- `expiry` (optional): Unix timestamp when records should expire (defaults to now + TTL)

**Success response:**

```json
{"ok": true, "message": "Added 2 records for test.example.com./A"}
```

**Error response (HTTP 400):**

```json
{"ok": false, "error": "Record 1 has different qname: expected a.example.com., got b.example.com."}
```

### Delete Records

**DELETE /records/delete** or **POST /records/delete**

Remove specific records from the buffer by matching exact RR values.

```bash
curl -X DELETE https://localhost:8053/records/delete \
  --cert client.crt \
  --key client.key \
  --cacert ca.crt \
  -H "Content-Type: application/json" \
  -d '{
    "records": [
      "test.example.com. 300 IN A 192.0.2.1"
    ]
  }'
```

**Request Fields:**

- `records` (required): Array of records in RFC1035 zone file format to delete. Only records that exactly match will be removed. QName and QType are automatically extracted.

**Success response:**

```json
{"ok": true, "message": "Deleted 1 records for test.example.com./A"}
```

### Health Check

**GET /health**

Check plugin health and buffer status.

```bash
curl https://localhost:8053/health \
  --cert client.crt \
  --key client.key \
  --cacert ca.crt
```

**Response:**

```json
{
  "status": "healthy",
  "buffer_size": 42,
  "plugin": "dynamicrecords",
  "instances": 1
}
```

## FrameStreams Ingestion

The optional FrameStreams channel (`fstrm_addr`) provides a persistent, streaming alternative to the HTTPS API for high-throughput ingestion.

### Protocol

- **Transport**: TLS 1.3 with mTLS (same certificates as the HTTPS listener)
- **ALPN**: `fstrm` — distinguishes this listener from the HTTPS listener at the TLS layer
- **Framing**: FrameStreams bidirectional protocol ([farsightsec/golang-framestream](https://github.com/farsightsec/golang-framestream))
- **Content-type**: `application/x-dynamicrecords` — negotiated in the FrameStreams handshake

### Frame payload

Each data frame contains a JSON object:

```json
{
  "op":      "add",
  "ttl":     300,
  "expiry":  1735689600,
  "records": ["svc.example.com. 60 IN A 10.0.0.1"]
}
```

| Field     | Type     | Required | Description |
|-----------|----------|----------|-------------|
| `op`      | string   | yes      | `"add"` or `"delete"` |
| `records` | string[] | yes      | RFC1035 zone file records; all must share the same qname and qtype |
| `ttl`     | uint32   | no       | TTL override (seconds); falls back to record TTL then `default_ttl` |
| `expiry`  | int64    | no       | Unix timestamp expiry; takes precedence over `ttl` |

### ACK/NACK responses

After every data frame the server writes a JSON response directly on the connection:

```
Client → DATA frame {"op":"add","records":["svc.example.com. 60 IN A 10.0.0.1"]}
Server → {"ok":true,"message":"Added 1 records for svc.example.com./A"}

Client → DATA frame {"op":"add","records":["bad record"]}
Server → {"ok":false,"error":"invalid record \"bad record\": dns: bad A A"}

Client → CONTROL_STOP
Server → CONTROL_FINISH
```

Responses are newline-delimited JSON written on the same TLS connection. The FrameStreams control frames (ACCEPT, FINISH) are handled by the library and do not interfere with the ACK stream.

### Example client (Go)

```go
conn, _ := tls.Dial("tcp", "localhost:8054", tlsConfig) // tlsConfig with client cert
w, _ := framestream.NewWriter(conn, &framestream.WriterOptions{
    ContentTypes:  [][]byte{[]byte("application/x-dynamicrecords")},
    Bidirectional: true,
})
dec := json.NewDecoder(conn)

frame, _ := json.Marshal(map[string]any{
    "op":      "add",
    "records": []string{"svc.example.com. 60 IN A 10.0.0.1"},
})
w.WriteFrame(frame)
w.Flush() // required — WriteFrame buffers internally

var ack map[string]any
dec.Decode(&ack) // {"ok":true,"message":"..."}

w.Close() // sends CONTROL_STOP, waits for CONTROL_FINISH
```

## mTLS Setup

### Generate Certificates

```bash
# Generate CA
openssl genrsa -out ca.key 4096
openssl req -new -x509 -days 3650 -key ca.key -out ca.crt \
  -subj "/CN=DynamicRecords CA"

# Generate server certificate
openssl genrsa -out server.key 4096
openssl req -new -key server.key -out server.csr \
  -subj "/CN=localhost"
openssl x509 -req -days 365 -in server.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out server.crt

# Generate client certificate
openssl genrsa -out client.key 4096
openssl req -new -key client.key -out client.csr \
  -subj "/CN=api-client"
openssl x509 -req -days 365 -in client.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out client.crt
```

## Building the Plugin

To use this plugin, you need to compile it into CoreDNS:

1. Create a `plugin.cfg` file or modify CoreDNS's existing one:

```
dynamicrecords:github.com/yourusername/dynamicrecords
```

2. Build CoreDNS with the plugin:

```bash
git clone https://github.com/coredns/coredns
cd coredns
echo "dynamicrecords:github.com/yourusername/dynamicrecords" >> plugin.cfg
go get github.com/yourusername/dynamicrecords
go generate
go build
```

## Example Use Cases

### Dynamic DNS Updates

Use the API to dynamically update DNS records without reloading CoreDNS:

```bash
# Add a temporary A record
curl -X POST https://localhost:8053/records \
  --cert client.crt --key client.key --cacert ca.crt \
  -H "Content-Type: application/json" \
  -d '{
    "expiry": '$(date -d "+1 hour" +%s)',
    "records": ["dynamic.example.com. 60 IN A 203.0.113.42"]
  }'
```

### Service Discovery

Populate records based on service registration:

```bash
# Register a service
curl -X POST https://localhost:8053/records \
  --cert client.crt --key client.key --cacert ca.crt \
  -H "Content-Type: application/json" \
  -d '{
    "records": ["api.service.local. 30 IN SRV 10 60 8080 server1.local."]
  }'
```

## Security Considerations

- **mTLS is mandatory**: All API requests require valid client certificates
- **TLS 1.3 only**: The server uses TLS 1.3 for maximum security
- **Client verification**: Server verifies client certificates against the configured CA
- **ALPN isolation**: The HTTPS listener advertises `h2`/`http/1.1`; the FrameStreams listener advertises `fstrm` — a client connecting to the wrong port will fail the TLS handshake
- **Network isolation**: Consider running the API on a separate internal network
- **Certificate management**: Rotate certificates regularly and revoke compromised ones

## Record Expiry

Records are automatically removed from the buffer when:
- The expiry time is reached (checked every minute)
- They are explicitly deleted via the API
- Expired records are never served in DNS responses

## Performance

- **Thread-safe**: Buffer uses read-write locks for concurrent access
- **Low latency**: In-memory storage provides fast lookups
- **Automatic cleanup**: Background goroutine removes expired entries every minute
- **Minimal overhead**: Only processes queries when downstream returns NODATA or NXDOMAIN

## Troubleshooting

### Records not being served

1. Check buffer contents via health endpoint
2. Verify records haven't expired
3. Ensure qname matches exactly (including trailing dot)
4. Confirm downstream plugin returns NODATA or NXDOMAIN
5. Check CoreDNS logs for errors

### mTLS connection failures

1. Verify certificate paths are correct
2. Check that client certificate is signed by the CA
3. Ensure certificates haven't expired
4. Verify server is listening on the configured address

### FrameStreams connection failures

1. Verify `fstrm_addr` is set in the Corefile
2. Confirm the client uses ALPN `fstrm` and content-type `application/x-dynamicrecords`
3. Check that the client calls `Flush()` after each `WriteFrame`
4. Review CoreDNS logs for handshake errors

### Plugin not loading

1. Check Corefile syntax
2. Ensure all required configuration fields are present
3. Review CoreDNS startup logs for errors
4. Verify plugin is compiled into the CoreDNS binary

## License

MIT
