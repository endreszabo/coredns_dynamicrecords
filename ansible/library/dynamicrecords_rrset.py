#!/usr/bin/python
# -*- coding: utf-8 -*-

# Copyright: (c) 2025, Endre Szabo
# GNU General Public License v3.0+ (see COPYING or https://www.gnu.org/licenses/gpl-3.0.txt)

from __future__ import absolute_import, division, print_function
__metaclass__ = type

DOCUMENTATION = r'''
---
module: dynamicrecords_rrset
short_description: Manage DNS records in CoreDNS DynamicRecords plugin
version_added: "1.0.0"
description:
    - Manages DNS resource record sets (RRsets) in the CoreDNS DynamicRecords plugin
    - Supports adding, updating, and deleting DNS records via the plugin's HTTPS API
    - Requires mTLS authentication with client certificates
    - Records have configurable TTL and expiry times
author:
    - Endre Szabo
options:
    state:
        description:
            - Whether the RRset should exist or not
        type: str
        choices: [ present, absent ]
        default: present
    api_url:
        description:
            - URL of the DynamicRecords API endpoint
            - Should include protocol and port (e.g., https://localhost:8053)
        type: str
        required: true
    records:
        description:
            - List of DNS records in RFC1035 zone file format
            - Required for both present and absent states
            - Each record should be a complete zone file entry
            - QName and QType are automatically extracted from the records
        type: list
        elements: str
        required: true
    ttl:
        description:
            - Optional TTL override in seconds
            - If not specified, uses TTL from the RFC1035 records or plugin's default_ttl
        type: int
        required: false
    expiry:
        description:
            - Unix timestamp when records should expire
            - If not specified, uses current time + TTL
        type: int
        required: false
    client_cert:
        description:
            - Path to client certificate file for mTLS authentication
        type: path
        required: true
    client_key:
        description:
            - Path to client private key file for mTLS authentication
        type: path
        required: true
    ca_cert:
        description:
            - Path to CA certificate file for server verification
        type: path
        required: true
    validate_certs:
        description:
            - Whether to validate SSL certificates
        type: bool
        default: true
    timeout:
        description:
            - Timeout for API requests in seconds
        type: int
        default: 30
notes:
    - Requires the requests library
    - All certificate paths must be accessible from the Ansible control node or target host
requirements:
    - requests >= 2.20.0
'''

EXAMPLES = r'''
# Add A records for a host
- name: Add A records for web server
  dynamicrecords_rrset:
    api_url: https://dns-server:8053
    records:
      - "web.example.com. 300 IN A 192.0.2.10"
      - "web.example.com. 300 IN A 192.0.2.11"
    client_cert: /etc/ssl/client.crt
    client_key: /etc/ssl/client.key
    ca_cert: /etc/ssl/ca.crt

# Add AAAA record with specific expiry
- name: Add temporary IPv6 record
  dynamicrecords_rrset:
    api_url: https://dns-server:8053
    ttl: 60
    expiry: "{{ ansible_date_time.epoch | int + 3600 }}"
    records:
      - "temp.example.com. 60 IN AAAA 2001:db8::1"
    client_cert: /etc/ssl/client.crt
    client_key: /etc/ssl/client.key
    ca_cert: /etc/ssl/ca.crt

# Add CNAME record
- name: Add CNAME for service
  dynamicrecords_rrset:
    api_url: https://dns-server:8053
    records:
      - "service.example.com. 300 IN CNAME web.example.com."
    client_cert: /etc/ssl/client.crt
    client_key: /etc/ssl/client.key
    ca_cert: /etc/ssl/ca.crt

# Add TXT record
- name: Add TXT record
  dynamicrecords_rrset:
    api_url: https://dns-server:8053
    records:
      - '_acme-challenge.example.com. 120 IN TXT "validation-token-here"'
    client_cert: /etc/ssl/client.crt
    client_key: /etc/ssl/client.key
    ca_cert: /etc/ssl/ca.crt

# Add SRV record for service discovery
- name: Add SRV record
  dynamicrecords_rrset:
    api_url: https://dns-server:8053
    records:
      - "_http._tcp.example.com. 300 IN SRV 10 60 8080 server1.example.com."
      - "_http._tcp.example.com. 300 IN SRV 10 40 8080 server2.example.com."
    client_cert: /etc/ssl/client.crt
    client_key: /etc/ssl/client.key
    ca_cert: /etc/ssl/ca.crt

# Delete specific records (matches by exact RR values)
- name: Remove specific DNS records
  dynamicrecords_rrset:
    state: absent
    api_url: https://dns-server:8053
    records:
      - "old.example.com. 300 IN A 192.0.2.99"
    client_cert: /etc/ssl/client.crt
    client_key: /etc/ssl/client.key
    ca_cert: /etc/ssl/ca.crt

# Add record without certificate validation (not recommended for production)
- name: Add record with validation disabled
  dynamicrecords_rrset:
    api_url: https://dns-server:8053
    records:
      - "test.example.com. 60 IN A 203.0.113.1"
    client_cert: /etc/ssl/client.crt
    client_key: /etc/ssl/client.key
    ca_cert: /etc/ssl/ca.crt
    validate_certs: false
'''

RETURN = r'''
message:
    description: Status message from the API
    type: str
    returned: always
    sample: "Added 2 records for web.example.com./A"
qname:
    description: The fully qualified domain name that was managed
    type: str
    returned: always
    sample: "web.example.com."
qtype:
    description: The record type that was managed
    type: str
    returned: always
    sample: "A"
record_count:
    description: Number of records added (only for state=present)
    type: int
    returned: when state=present
    sample: 2
api_response:
    description: Full API response from the server
    type: dict
    returned: on success
    sample: {"status": "success", "message": "Added 2 records for web.example.com./A"}
'''

import json
import traceback

try:
    import requests
    HAS_REQUESTS = True
except ImportError:
    HAS_REQUESTS = False
    REQUESTS_IMPORT_ERROR = traceback.format_exc()

from ansible.module_utils.basic import AnsibleModule, missing_required_lib


def add_records(module, api_url, records, ttl, expiry,
                client_cert, client_key, ca_cert, validate_certs, timeout):
    """Add or update records via the API"""

    url = f"{api_url}/records"

    payload = {
        "records": records
    }

    if ttl is not None:
        payload["ttl"] = ttl

    if expiry is not None:
        payload["expiry"] = expiry

    try:
        response = requests.post(
            url,
            json=payload,
            cert=(client_cert, client_key),
            verify=ca_cert if validate_certs else False,
            timeout=timeout
        )

        response.raise_for_status()

        result = response.json()
        return True, result

    except requests.exceptions.SSLError as e:
        module.fail_json(msg=f"SSL/TLS error: {str(e)}")
    except requests.exceptions.ConnectionError as e:
        module.fail_json(msg=f"Connection error: {str(e)}")
    except requests.exceptions.Timeout as e:
        module.fail_json(msg=f"Request timeout: {str(e)}")
    except requests.exceptions.HTTPError as e:
        module.fail_json(msg=f"HTTP error: {e.response.status_code} - {e.response.text}")
    except Exception as e:
        module.fail_json(msg=f"Unexpected error: {str(e)}")


def delete_records(module, api_url, records,
                   client_cert, client_key, ca_cert, validate_certs, timeout):
    """Delete records via the API"""

    url = f"{api_url}/records/delete"

    payload = {
        "records": records
    }

    try:
        response = requests.delete(
            url,
            json=payload,
            cert=(client_cert, client_key),
            verify=ca_cert if validate_certs else False,
            timeout=timeout
        )

        response.raise_for_status()

        result = response.json()
        return True, result

    except requests.exceptions.SSLError as e:
        module.fail_json(msg=f"SSL/TLS error: {str(e)}")
    except requests.exceptions.ConnectionError as e:
        module.fail_json(msg=f"Connection error: {str(e)}")
    except requests.exceptions.Timeout as e:
        module.fail_json(msg=f"Request timeout: {str(e)}")
    except requests.exceptions.HTTPError as e:
        module.fail_json(msg=f"HTTP error: {e.response.status_code} - {e.response.text}")
    except Exception as e:
        module.fail_json(msg=f"Unexpected error: {str(e)}")


def check_health(api_url, client_cert, client_key, ca_cert, validate_certs, timeout):
    """Check if the API is accessible"""
    url = f"{api_url}/health"

    try:
        response = requests.get(
            url,
            cert=(client_cert, client_key),
            verify=ca_cert if validate_certs else False,
            timeout=timeout
        )
        response.raise_for_status()
        return True, response.json()
    except:
        return False, None


def run_module():
    module_args = dict(
        state=dict(type='str', default='present', choices=['present', 'absent']),
        api_url=dict(type='str', required=True),
        records=dict(type='list', elements='str', required=True),
        ttl=dict(type='int', required=False),
        expiry=dict(type='int', required=False),
        client_cert=dict(type='path', required=True),
        client_key=dict(type='path', required=True),
        ca_cert=dict(type='path', required=True),
        validate_certs=dict(type='bool', default=True),
        timeout=dict(type='int', default=30),
    )

    result = dict(
        changed=False,
        message='',
        qname='',
        qtype='',
    )

    module = AnsibleModule(
        argument_spec=module_args,
        supports_check_mode=True
    )

    if not HAS_REQUESTS:
        module.fail_json(msg=missing_required_lib('requests'),
                        exception=REQUESTS_IMPORT_ERROR)

    state = module.params['state']
    api_url = module.params['api_url'].rstrip('/')
    records = module.params['records']
    ttl = module.params['ttl']
    expiry = module.params['expiry']
    client_cert = module.params['client_cert']
    client_key = module.params['client_key']
    ca_cert = module.params['ca_cert']
    validate_certs = module.params['validate_certs']
    timeout = module.params['timeout']

    # Parse first record to extract qname and qtype for output
    # The API will validate and extract these values
    try:
        # Simple parsing to extract qname and qtype for display
        first_record = records[0].split()
        if len(first_record) >= 4:
            qname = first_record[0]
            qtype = first_record[3]
        else:
            qname = "unknown"
            qtype = "unknown"
    except:
        qname = "unknown"
        qtype = "unknown"

    result['qname'] = qname
    result['qtype'] = qtype

    # Check API connectivity
    healthy, health_data = check_health(api_url, client_cert, client_key,
                                        ca_cert, validate_certs, timeout)
    if not healthy:
        module.fail_json(msg=f"Cannot connect to API at {api_url}. "
                            "Check URL, certificates, and network connectivity.")

    if module.check_mode:
        result['changed'] = True
        result['message'] = f"Would {'add' if state == 'present' else 'delete'} records for {qname}/{qtype}"
        module.exit_json(**result)

    if state == 'present':
        # Add or update records
        success, api_response = add_records(
            module, api_url, records, ttl, expiry,
            client_cert, client_key, ca_cert, validate_certs, timeout
        )

        if success:
            result['changed'] = True
            result['message'] = api_response.get('message', 'Records added successfully')
            result['record_count'] = len(records)
            result['api_response'] = api_response

    elif state == 'absent':
        # Delete records
        success, api_response = delete_records(
            module, api_url, records,
            client_cert, client_key, ca_cert, validate_certs, timeout
        )

        if success:
            result['changed'] = True
            result['message'] = api_response.get('message', 'Records deleted successfully')
            result['api_response'] = api_response

    module.exit_json(**result)


def main():
    run_module()


if __name__ == '__main__':
    main()
