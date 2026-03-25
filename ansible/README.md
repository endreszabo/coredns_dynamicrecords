# DynamicRecords Ansible Module

Ansible module for managing DNS records in the CoreDNS DynamicRecords plugin.

## Features

- Add, update, and delete DNS records via Ansible playbooks
- Full mTLS support with client certificate authentication
- Support for all common DNS record types (A, AAAA, CNAME, TXT, SRV, MX, etc.)
- Configurable TTL and expiry times
- Idempotent operations
- Check mode support
- Connection health checking

## Installation

### Option 1: Use the module directly

Place the module in your playbook's `library` directory:

```bash
mkdir -p playbooks/library
cp library/dynamicrecords_rrset.py playbooks/library/
```

### Option 2: Configure Ansible library path

Add to your `ansible.cfg`:

```ini
[defaults]
library = /path/to/dynamicrecords/ansible/library
```

### Option 3: Set environment variable

```bash
export ANSIBLE_LIBRARY=/path/to/dynamicrecords/ansible/library
```

## Requirements

- Python 3.6 or later
- `requests` library (install with `pip install requests`)
- Valid client certificates for mTLS authentication
- Access to the DynamicRecords API endpoint

Install Python dependencies:

```bash
pip install -r requirements.txt
```

## Module: dynamicrecords_rrset

### Parameters

| Parameter | Required | Type | Default | Description |
|-----------|----------|------|---------|-------------|
| `state` | No | str | `present` | Whether the RRset should exist (`present`) or not (`absent`) |
| `api_url` | Yes | str | - | URL of the DynamicRecords API (e.g., `https://dns:8053`) |
| `qname` | Yes | str | - | Fully qualified domain name (with trailing dot) |
| `qtype` | Yes | str | - | DNS record type (A, AAAA, CNAME, TXT, etc.) |
| `records` | Conditional | list | - | List of records in RFC1035 format (required when `state=present`) |
| `ttl` | No | int | - | TTL in seconds (uses plugin default if not specified) |
| `expiry` | No | int | - | Unix timestamp for expiry (defaults to now + TTL) |
| `client_cert` | Yes | path | - | Path to client certificate file |
| `client_key` | Yes | path | - | Path to client private key file |
| `ca_cert` | Yes | path | - | Path to CA certificate file |
| `validate_certs` | No | bool | `true` | Whether to validate SSL certificates |
| `timeout` | No | int | `30` | API request timeout in seconds |

### Return Values

| Key | Type | Description |
|-----|------|-------------|
| `changed` | bool | Whether the record was modified |
| `message` | str | Status message from the operation |
| `qname` | str | The FQDN that was managed |
| `qtype` | str | The record type that was managed |
| `record_count` | int | Number of records added (present state only) |
| `api_response` | dict | Full API response |

## Usage Examples

### Basic A Record

```yaml
- name: Add A record
  dynamicrecords_rrset:
    api_url: https://dns-server:8053
    qname: web.example.com.
    qtype: A
    records:
      - "web.example.com. 300 IN A 192.0.2.10"
    client_cert: /etc/ssl/client.crt
    client_key: /etc/ssl/client.key
    ca_cert: /etc/ssl/ca.crt
```

### Multiple A Records (Load Balancing)

```yaml
- name: Add multiple A records for load balancing
  dynamicrecords_rrset:
    api_url: https://dns-server:8053
    qname: app.example.com.
    qtype: A
    ttl: 60
    records:
      - "app.example.com. 60 IN A 192.0.2.10"
      - "app.example.com. 60 IN A 192.0.2.11"
      - "app.example.com. 60 IN A 192.0.2.12"
    client_cert: /etc/ssl/client.crt
    client_key: /etc/ssl/client.key
    ca_cert: /etc/ssl/ca.crt
```

### AAAA Record (IPv6)

```yaml
- name: Add IPv6 record
  dynamicrecords_rrset:
    api_url: https://dns-server:8053
    qname: ipv6.example.com.
    qtype: AAAA
    records:
      - "ipv6.example.com. 300 IN AAAA 2001:db8::1"
    client_cert: /etc/ssl/client.crt
    client_key: /etc/ssl/client.key
    ca_cert: /etc/ssl/ca.crt
```

### CNAME Record

```yaml
- name: Add CNAME record
  dynamicrecords_rrset:
    api_url: https://dns-server:8053
    qname: www.example.com.
    qtype: CNAME
    records:
      - "www.example.com. 300 IN CNAME web.example.com."
    client_cert: /etc/ssl/client.crt
    client_key: /etc/ssl/client.key
    ca_cert: /etc/ssl/ca.crt
```

### TXT Record

```yaml
- name: Add TXT record for domain verification
  dynamicrecords_rrset:
    api_url: https://dns-server:8053
    qname: _acme-challenge.example.com.
    qtype: TXT
    ttl: 120
    records:
      - '_acme-challenge.example.com. 120 IN TXT "verification-token-12345"'
    client_cert: /etc/ssl/client.crt
    client_key: /etc/ssl/client.key
    ca_cert: /etc/ssl/ca.crt
```

### SRV Record (Service Discovery)

```yaml
- name: Add SRV records for HTTP service
  dynamicrecords_rrset:
    api_url: https://dns-server:8053
    qname: _http._tcp.example.com.
    qtype: SRV
    records:
      - "_http._tcp.example.com. 300 IN SRV 10 60 8080 server1.example.com."
      - "_http._tcp.example.com. 300 IN SRV 10 40 8080 server2.example.com."
    client_cert: /etc/ssl/client.crt
    client_key: /etc/ssl/client.key
    ca_cert: /etc/ssl/ca.crt
```

### MX Record

```yaml
- name: Add MX records
  dynamicrecords_rrset:
    api_url: https://dns-server:8053
    qname: example.com.
    qtype: MX
    records:
      - "example.com. 300 IN MX 10 mail1.example.com."
      - "example.com. 300 IN MX 20 mail2.example.com."
    client_cert: /etc/ssl/client.crt
    client_key: /etc/ssl/client.key
    ca_cert: /etc/ssl/ca.crt
```

### Temporary Record with Expiry

```yaml
- name: Add temporary record that expires in 1 hour
  dynamicrecords_rrset:
    api_url: https://dns-server:8053
    qname: temp.example.com.
    qtype: A
    ttl: 60
    expiry: "{{ ansible_date_time.epoch | int + 3600 }}"
    records:
      - "temp.example.com. 60 IN A 203.0.113.50"
    client_cert: /etc/ssl/client.crt
    client_key: /etc/ssl/client.key
    ca_cert: /etc/ssl/ca.crt
```

### Delete Record

```yaml
- name: Delete DNS records
  dynamicrecords_rrset:
    state: absent
    api_url: https://dns-server:8053
    qname: old.example.com.
    qtype: A
    client_cert: /etc/ssl/client.crt
    client_key: /etc/ssl/client.key
    ca_cert: /etc/ssl/ca.crt
```

### Using Variables

```yaml
- name: Add record using variables
  dynamicrecords_rrset:
    api_url: "{{ dns_api_url }}"
    qname: "{{ inventory_hostname }}."
    qtype: A
    ttl: "{{ dns_ttl | default(300) }}"
    records:
      - "{{ inventory_hostname }}. {{ dns_ttl | default(300) }} IN A {{ ansible_default_ipv4.address }}"
    client_cert: "{{ dns_client_cert }}"
    client_key: "{{ dns_client_key }}"
    ca_cert: "{{ dns_ca_cert }}"
```

## Complete Playbook Examples

See the [playbooks](playbooks/) directory for complete examples:

- [basic.yml](playbooks/basic.yml) - Basic usage examples
- [service-registration.yml](playbooks/service-registration.yml) - Dynamic service registration
- [load-balancer.yml](playbooks/load-balancer.yml) - Load balancer pool management

## Certificate Management

### Using Certificate Variables

Define certificates in your inventory or group_vars:

```yaml
# group_vars/all.yml
dns_api_url: https://dns-server.example.com:8053
dns_client_cert: /etc/ssl/certs/dns-client.crt
dns_client_key: /etc/ssl/private/dns-client.key
dns_ca_cert: /etc/ssl/certs/dns-ca.crt
```

### Using Ansible Vault for Sensitive Data

Encrypt certificate paths or certificates themselves:

```bash
ansible-vault encrypt_string '/etc/ssl/private/dns-client.key' --name 'dns_client_key'
```

## Error Handling

The module provides detailed error messages for common issues:

```yaml
- name: Add record with error handling
  dynamicrecords_rrset:
    api_url: https://dns-server:8053
    qname: test.example.com.
    qtype: A
    records:
      - "test.example.com. 300 IN A 192.0.2.1"
    client_cert: /etc/ssl/client.crt
    client_key: /etc/ssl/client.key
    ca_cert: /etc/ssl/ca.crt
  register: result
  failed_when: false

- name: Display result
  debug:
    var: result
```

## Check Mode

The module supports Ansible's check mode:

```bash
ansible-playbook playbook.yml --check
```

## Testing

### Test Connectivity

```yaml
- name: Test API connectivity
  dynamicrecords_rrset:
    api_url: https://dns-server:8053
    qname: test.example.com.
    qtype: A
    records:
      - "test.example.com. 60 IN A 127.0.0.1"
    client_cert: /etc/ssl/client.crt
    client_key: /etc/ssl/client.key
    ca_cert: /etc/ssl/ca.crt
  check_mode: yes
```

### Verify with dig

After adding records, verify with dig:

```bash
dig @dns-server test.example.com A
```

## Troubleshooting

### Certificate Errors

If you get SSL/TLS errors:

1. Verify certificate files exist and are readable
2. Check certificate validity: `openssl x509 -in client.crt -text -noout`
3. Verify client certificate is signed by the CA
4. Ensure certificates haven't expired

### Connection Errors

If you get connection errors:

1. Verify the API URL is correct
2. Check network connectivity: `curl -k https://dns-server:8053/health`
3. Verify the DynamicRecords plugin is running
4. Check firewall rules

### Invalid Record Format

Records must be in RFC1035 zone file format:

```
# Correct
"example.com. 300 IN A 192.0.2.1"

# Incorrect
"192.0.2.1"
```

## Integration Examples

### Dynamic Service Registration

Register services automatically during deployment:

```yaml
- name: Register web service in DNS
  dynamicrecords_rrset:
    api_url: "{{ dns_api_url }}"
    qname: "web-{{ inventory_hostname_short }}.service.local."
    qtype: A
    ttl: 60
    records:
      - "web-{{ inventory_hostname_short }}.service.local. 60 IN A {{ ansible_default_ipv4.address }}"
    client_cert: "{{ dns_client_cert }}"
    client_key: "{{ dns_client_key }}"
    ca_cert: "{{ dns_ca_cert }}"
```

### Load Balancer Pool Updates

Update load balancer pools dynamically:

```yaml
- name: Update load balancer pool
  dynamicrecords_rrset:
    api_url: "{{ dns_api_url }}"
    qname: app.example.com.
    qtype: A
    ttl: 30
    records: "{{ groups['webservers'] | map('extract', hostvars, ['ansible_default_ipv4', 'address']) | map('regex_replace', '^(.*)$', 'app.example.com. 30 IN A \\1') | list }}"
    client_cert: "{{ dns_client_cert }}"
    client_key: "{{ dns_client_key }}"
    ca_cert: "{{ dns_ca_cert }}"
```

### Container/Kubernetes Integration

Register containers as they start:

```yaml
- name: Register container in DNS
  dynamicrecords_rrset:
    api_url: "{{ dns_api_url }}"
    qname: "{{ container_name }}.containers.local."
    qtype: A
    ttl: 30
    expiry: "{{ ansible_date_time.epoch | int + container_lifetime }}"
    records:
      - "{{ container_name }}.containers.local. 30 IN A {{ container_ip }}"
    client_cert: "{{ dns_client_cert }}"
    client_key: "{{ dns_client_key }}"
    ca_cert: "{{ dns_ca_cert }}"
```

## Contributing

Contributions are welcome! Please ensure:

1. Module follows Ansible module conventions
2. Documentation is updated
3. Examples are tested and working
4. Error handling is comprehensive

## License

GNU General Public License v3.0
