#!/usr/bin/env python3
"""
Provision SuperMQ resources for FL demo.
This script creates the necessary domain, clients, and channel for the demo.
"""
import requests
import json
import sys
import time
import re
from pathlib import Path

# SuperMQ service URLs (from compose file)
USERS_URL = "http://localhost:9002"
DOMAINS_URL = "http://localhost:9003"
CLIENTS_URL = "http://localhost:9006"
CHANNELS_URL = "http://localhost:9005"

# Default admin credentials (from SuperMQ .env defaults)
ADMIN_USERNAME = "admin"
ADMIN_PASSWORD = "12345678"

# Demo configuration
DOMAIN_NAME = "fl-demo"
DOMAIN_ROUTE = "fl-demo"
CHANNEL_NAME = "fl"
CLIENT_NAMES = ["manager", "proplet-1", "proplet-2", "proplet-3", "fl-coordinator"]


def wait_for_service(url, name, max_retries=30):
    """Wait for a service to be available."""
    print(f"Waiting for {name} service...")
    for i in range(max_retries):
        try:
            response = requests.get(f"{url}/health", timeout=2)
            if response.status_code in [200, 404]:  # 404 is ok, means service is up
                print(f"✓ {name} service is ready")
                return True
        except requests.exceptions.RequestException:
            pass
        time.sleep(1)
    print(f"✗ {name} service did not become available")
    return False


def login():
    """Login and get access token."""
    print("\n=== Logging in ===")
    login_data = {
        "username": ADMIN_USERNAME,
        "password": ADMIN_PASSWORD
    }
    
    try:
        response = requests.post(
            f"{USERS_URL}/users/tokens/issue",
            json=login_data,
            headers={"Content-Type": "application/json"},
            timeout=10
        )
        response.raise_for_status()
        token_data = response.json()
        access_token = token_data.get("access_token") or token_data.get("accessToken")
        if not access_token:
            print(f"Error: No access token in response: {token_data}")
            return None
        print("✓ Login successful")
        return access_token
    except requests.exceptions.RequestException as e:
        print(f"✗ Login failed: {e}")
        if hasattr(e.response, 'text'):
            print(f"  Response: {e.response.text}")
        return None


def get_existing_domain(token):
    """Check if domain already exists by route."""
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json"
    }
    
    try:
        # List all domains and check if route exists
        response = requests.get(
            f"{DOMAINS_URL}/domains",
            headers=headers,
            timeout=10
        )
        response.raise_for_status()
        domains_data = response.json()
        domains_list = domains_data.get("domains", [])
        
        for d in domains_list:
            if d.get("route") == DOMAIN_ROUTE or d.get("name") == DOMAIN_NAME:
                print(f"✓ Found existing domain: {d.get('id')} (route: {d.get('route')})")
                return d
        return None
    except requests.exceptions.RequestException as e:
        # If we can't list domains, continue and try to create
        return None


def create_domain(token):
    """Create or get domain."""
    print("\n=== Creating Domain ===")
    
    # First, check if domain already exists
    existing_domain = get_existing_domain(token)
    if existing_domain:
        return existing_domain
    
    # Domain data - note: "permission" is not a valid field in the API
    domain_data = {
        "name": DOMAIN_NAME,
        "route": DOMAIN_ROUTE
    }
    
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json"
    }
    
    try:
        # Try to create domain
        response = requests.post(
            f"{DOMAINS_URL}/domains",
            json=domain_data,
            headers=headers,
            timeout=10
        )
        
        if response.status_code == 201:
            domain = response.json()
            print(f"✓ Domain created: {domain.get('id')}")
            return domain
        elif response.status_code == 400:
            # Check if it's a "route not available" error
            error_text = response.text.lower()
            if "route not available" in error_text or "route" in error_text:
                print("Route already exists, fetching existing domain...")
                # Try to get the existing domain
                existing = get_existing_domain(token)
                if existing:
                    return existing
                print("✗ Route exists but could not retrieve domain")
                return None
            else:
                print(f"✗ Bad request: {response.text}")
                return None
        elif response.status_code == 409:
            # Domain already exists (ID conflict)
            print("Domain ID already exists, fetching...")
            existing = get_existing_domain(token)
            if existing:
                return existing
            print("✗ Domain exists but could not retrieve it")
            return None
        else:
            response.raise_for_status()
    except requests.exceptions.RequestException as e:
        print(f"✗ Failed to create domain: {e}")
        if hasattr(e, 'response') and e.response is not None:
            print(f"  Response: {e.response.text}")
        return None


def create_client(token, domain_id, client_name):
    """Create a client."""
    client_data = {
        "name": client_name,
        "tags": ["propeller", "fl-demo"],
        "status": "enabled"
    }
    
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json"
    }
    
    try:
        response = requests.post(
            f"{CLIENTS_URL}/{domain_id}/clients",
            json=client_data,
            headers=headers,
            timeout=10
        )
        
        if response.status_code == 201:
            client = response.json()
            print(f"✓ Client created: {client_name} (ID: {client.get('id')})")
            return client
        elif response.status_code == 409:
            # Client already exists, try to get it
            print(f"  Client {client_name} already exists, fetching...")
            response = requests.get(
                f"{CLIENTS_URL}/{domain_id}/clients",
                headers=headers,
                params={"name": client_name},
                timeout=10
            )
            response.raise_for_status()
            clients = response.json().get("clients", [])
            for c in clients:
                if c.get("name") == client_name:
                    print(f"✓ Using existing client: {client_name} (ID: {c.get('id')})")
                    return c
            print(f"✗ Client {client_name} exists but could not retrieve it")
            return None
        else:
            response.raise_for_status()
    except requests.exceptions.RequestException as e:
        print(f"✗ Failed to create client {client_name}: {e}")
        if hasattr(e, 'response') and e.response is not None:
            print(f"  Response: {e.response.text}")
        return None


def create_channel(token, domain_id):
    """Create or get channel."""
    print("\n=== Creating Channel ===")
    channel_data = {
        "name": CHANNEL_NAME,
        "status": "enabled"
    }
    
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json"
    }
    
    try:
        response = requests.post(
            f"{CHANNELS_URL}/{domain_id}/channels",
            json=channel_data,
            headers=headers,
            timeout=10
        )
        
        if response.status_code == 201:
            channel = response.json()
            print(f"✓ Channel created: {channel.get('id')}")
            return channel
        elif response.status_code == 409:
            # Channel already exists, try to get it
            print("Channel already exists, fetching...")
            response = requests.get(
                f"{CHANNELS_URL}/{domain_id}/channels",
                headers=headers,
                params={"name": CHANNEL_NAME},
                timeout=10
            )
            response.raise_for_status()
            channels = response.json().get("channels", [])
            for c in channels:
                if c.get("name") == CHANNEL_NAME:
                    print(f"✓ Using existing channel: {c.get('id')}")
                    return c
            print("✗ Channel exists but could not retrieve it")
            return None
        else:
            response.raise_for_status()
    except requests.exceptions.RequestException as e:
        print(f"✗ Failed to create channel: {e}")
        if hasattr(e, 'response') and e.response is not None:
            print(f"  Response: {e.response.text}")
        return None


def connect_clients_to_channel(token, domain_id, client_ids, channel_id):
    """Connect clients to channel."""
    print("\n=== Connecting Clients to Channel ===")
    # Note: The API expects channel_ids and client_ids (with underscores)
    # and types must be valid connection types
    connection_data = {
        "client_ids": client_ids,
        "channel_ids": [channel_id],
        "types": ["publish", "subscribe"]
    }
    
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json"
    }
    
    try:
        response = requests.post(
            f"{CHANNELS_URL}/{domain_id}/channels/connect",
            json=connection_data,
            headers=headers,
            timeout=10
        )
        
        if response.status_code in [200, 201]:
            print(f"✓ Connected {len(client_ids)} clients to channel")
            return True
        else:
            response.raise_for_status()
    except requests.exceptions.RequestException as e:
        print(f"✗ Failed to connect clients: {e}")
        if hasattr(e, 'response') and e.response is not None:
            print(f"  Response: {e.response.text}")
        return False


def update_compose_file(compose_file, clients, domain_id, channel_id):
    """Update compose.yaml with new client credentials."""
    if not compose_file.exists():
        print(f"Warning: Compose file not found: {compose_file}")
        return False
    
    content = compose_file.read_text()
    original_content = content
    
    # Map client names to their corresponding environment variable prefixes
    client_mapping = {
        'manager': {
            'id_var': 'MANAGER_CLIENT_ID',
            'key_var': 'MANAGER_CLIENT_KEY'
        },
        'proplet-1': {
            'id_var': 'PROPLET_CLIENT_ID',
            'key_var': 'PROPLET_CLIENT_KEY',
            'section': 'proplet'  # The first proplet is just 'proplet' in compose file
        },
        'proplet-2': {
            'id_var': 'PROPLET_CLIENT_ID',
            'key_var': 'PROPLET_CLIENT_KEY',
            'section': 'proplet-2'
        },
        'proplet-3': {
            'id_var': 'PROPLET_CLIENT_ID',
            'key_var': 'PROPLET_CLIENT_KEY',
            'section': 'proplet-3'
        }
    }
    
    # Update domain ID if needed
    domain_pattern = r'(MANAGER_DOMAIN_ID:\s*\${{MANAGER_DOMAIN_ID:-)([a-f0-9-]+)(}})'
    def replace_domain(match):
        return f"{match.group(1)}{domain_id}{match.group(3)}"
    content = re.sub(domain_pattern, replace_domain, content)
    
    # Update all PROPLET_DOMAIN_ID occurrences
    proplet_domain_pattern = r'(PROPLET_DOMAIN_ID:\s*\${{PROPLET_DOMAIN_ID:-)([a-f0-9-]+)(}})'
    def replace_proplet_domain(match):
        return f"{match.group(1)}{domain_id}{match.group(3)}"
    content = re.sub(proplet_domain_pattern, replace_proplet_domain, content)
    
    # Update channel ID if needed
    channel_pattern = r'(MANAGER_CHANNEL_ID:\s*\${{MANAGER_CHANNEL_ID:-)([a-f0-9-]+)(}})'
    def replace_channel(match):
        return f"{match.group(1)}{channel_id}{match.group(3)}"
    content = re.sub(channel_pattern, replace_channel, content)
    
    # Update all PROPLET_CHANNEL_ID occurrences
    proplet_channel_pattern = r'(PROPLET_CHANNEL_ID:\s*\${{PROPLET_CHANNEL_ID:-)([a-f0-9-]+)(}})'
    def replace_proplet_channel(match):
        return f"{match.group(1)}{channel_id}{match.group(3)}"
    content = re.sub(proplet_channel_pattern, replace_proplet_channel, content)
    
    # Update client credentials
    for client_name, client in clients.items():
        if client_name not in client_mapping:
            continue
        
        mapping = client_mapping[client_name]
        id_var = mapping['id_var']
        key_var = mapping['key_var']
        
        client_id = client.get("id")
        client_key = client.get("credentials", {}).get("secret", "N/A")
        
        if not client_id or client_key == "N/A":
            continue
        
        # Pattern to match the environment variable lines
        # Match: MANAGER_CLIENT_ID: ${MANAGER_CLIENT_ID:-old-value}
        id_pattern = rf'({id_var}:\s*\${{{id_var}:-)([a-f0-9-]+)(}})'
        key_pattern = rf'({key_var}:\s*\${{{key_var}:-)([a-f0-9-]+)(}})'
        
        # Replace ID using a function to avoid regex group reference issues
        def replace_id(match):
            return f"{match.group(1)}{client_id}{match.group(3)}"
        
        # Replace Key using a function to avoid regex group reference issues
        def replace_key(match):
            return f"{match.group(1)}{client_key}{match.group(3)}"
        
        content = re.sub(id_pattern, replace_id, content)
        content = re.sub(key_pattern, replace_key, content)
    
    if content != original_content:
        # Create backup
        backup_path = compose_file.with_suffix('.yaml.bak')
        if backup_path.exists():
            backup_path.unlink()  # Remove old backup
        compose_file.rename(backup_path)
        print(f"  Created backup: {backup_path.name}")
        
        # Write updated content
        compose_file.write_text(content)
        return True
    
    return False


def main():
    print("=" * 60)
    print("SuperMQ Provisioning Script for FL Demo")
    print("=" * 60)
    
    # Wait for services
    if not wait_for_service(USERS_URL, "Users"):
        sys.exit(1)
    if not wait_for_service(DOMAINS_URL, "Domains"):
        sys.exit(1)
    if not wait_for_service(CLIENTS_URL, "Clients"):
        sys.exit(1)
    if not wait_for_service(CHANNELS_URL, "Channels"):
        sys.exit(1)
    
    # Login
    token = login()
    if not token:
        print("\n✗ Provisioning failed: Could not login")
        sys.exit(1)
    
    # Create domain
    domain = create_domain(token)
    if not domain:
        print("\n✗ Provisioning failed: Could not create domain")
        sys.exit(1)
    domain_id = domain.get("id")
    
    # Create clients
    print("\n=== Creating Clients ===")
    clients = {}
    for client_name in CLIENT_NAMES:
        client = create_client(token, domain_id, client_name)
        if client:
            clients[client_name] = client
        else:
            print(f"⚠ Warning: Could not create client {client_name}")
    
    if not clients:
        print("\n✗ Provisioning failed: No clients created")
        sys.exit(1)
    
    # Create channel
    channel = create_channel(token, domain_id)
    if not channel:
        print("\n✗ Provisioning failed: Could not create channel")
        sys.exit(1)
    channel_id = channel.get("id")
    
    # Connect clients to channel
    client_ids = [c.get("id") for c in clients.values() if c.get("id")]
    if not connect_clients_to_channel(token, domain_id, client_ids, channel_id):
        print("\n⚠ Warning: Could not connect all clients to channel")
    
    # Print summary
    print("\n" + "=" * 60)
    print("Provisioning Summary")
    print("=" * 60)
    print(f"Domain ID: {domain_id}")
    print(f"Channel ID: {channel_id}")
    print("\nClients:")
    for name, client in clients.items():
        client_id = client.get("id")
        client_key = client.get("credentials", {}).get("secret", "N/A")
        print(f"  {name}:")
        print(f"    ID: {client_id}")
        print(f"    Key: {client_key}")
    
    print("\n✓ Provisioning completed successfully!")
    
    # Update compose.yaml with new credentials
    compose_file = Path(__file__).parent / "compose.yaml"
    if update_compose_file(compose_file, clients, domain_id, channel_id):
        print("\n✓ Updated compose.yaml with new credentials")
    else:
        print("\n⚠ Could not update compose.yaml automatically")
        print("   Please update it manually with the credentials shown above")
    
    print("\nNote: Restart services to apply new credentials:")
    print("  docker compose -f docker/compose.yaml -f examples/fl-demo/compose.yaml --env-file docker/.env restart manager proplet proplet-2 proplet-3")


if __name__ == "__main__":
    main()
