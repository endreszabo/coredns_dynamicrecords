# DynamicRecords - CoreDNS Plugin

A CoreDNS plugin that serves DNS records from an in-memory buffer populated via an mTLS-protected HTTPS API. Records have configurable expiry times and are automatically cleaned up when expired.

## Features

- **In-memory buffer**: Fast DNS record storage with automatic expiry
- **mTLS authentication**: Secure HTTPS API with mutual TLS authentication
- **RFC1035 format**: Accept records in standard zone file format
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
        http_addr :8053
        cert /path/to/server.crt
        key /path/to/server.key
        ca /path/to/ca.crt
        default_ttl 300
    }
    forward . 8.8.8.8
}
```

### Configuration Options

- **http_addr**: Address for the HTTPS API server (default: `:8053`)
- **cert**: Path to server TLS certificate (required)
- **key**: Path to server TLS private key (required)
- **ca**: Path to CA certificate for client verification (required)
- **default_ttl**: Default TTL in seconds if not specified in API request (default: `300`)

### Multiple Domains (Shared HTTP Server)

The plugin supports multiple server blocks (domains) while using a **single shared HTTP server**. All plugin instances share the same in-memory buffer and HTTP listener:

```
example.com {
    dynamicrecords {
        http_addr :8053
        cert /path/to/server.crt
        key /path/to/server.key
        ca /path/to/ca.crt
        default_ttl 300
    }
    forward . 8.8.8.8
}

example.org {
    dynamicrecords {
        http_addr :8053
        cert /path/to/server.crt
        key /path/to/server.key
        ca /path/to/ca.crt
        default_ttl 300
    }
    forward . 8.8.8.8
}

*.internal {
    dynamicrecords {
        http_addr :8053
        cert /path/to/server.crt
        key /path/to/server.key
        ca /path/to/ca.crt
        default_ttl 60
    }
    forward . 10.0.0.1
}
```

**Key Points:**
- All instances **must use the same `http_addr`** (e.g., `:8053`)
- All instances **must use the same certificate files** (cert, key, ca)
- The shared server starts when the first plugin instance loads
- The shared server stops when the last plugin instance shuts down
- All domains share the same in-memory buffer
- Records can be served across any domain (e.g., add a record for `test.example.com` and it will be served when querying the `example.com` server block)

## API Endpoints

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

**Response:**

```json
{
  "status": "success",
  "message": "Added 2 records for test.example.com./A"
}
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
  "plugin": "dynamicrecords"
}
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

### Plugin not loading

1. Check Corefile syntax
2. Ensure all required configuration fields are present
3. Review CoreDNS startup logs for errors
4. Verify plugin is compiled into the CoreDNS binary

## License

MIT
