#!/usr/bin/env python3
"""
Clear provisioned IDs from docker/.env file.
This script sets the environment variables (that were set by provision-smq.py) to empty values,
keeping the variable names but removing their values (e.g., MANAGER_DOMAIN_ID=).
"""
import re
import sys
from pathlib import Path

# Environment variables set by the provisioning script
PROVISIONED_VARS = [
    # Client IDs
    'MANAGER_CLIENT_ID',
    'MANAGER_CLIENT_KEY',
    'PROPLET_CLIENT_ID',
    'PROPLET_CLIENT_KEY',
    'PROPLET_2_CLIENT_ID',
    'PROPLET_2_CLIENT_KEY',
    'PROPLET_3_CLIENT_ID',
    'PROPLET_3_CLIENT_KEY',
    'COORDINATOR_CLIENT_ID',
    'COORDINATOR_CLIENT_KEY',
    'PROXY_CLIENT_ID',
    'PROXY_CLIENT_KEY',
    # Domain IDs
    'MANAGER_DOMAIN_ID',
    'PROPLET_DOMAIN_ID',
    'PROXY_DOMAIN_ID',
    # Channel IDs
    'MANAGER_CHANNEL_ID',
    'PROPLET_CHANNEL_ID',
    'PROXY_CHANNEL_ID',
]


def clear_env_vars(env_file, vars_to_clear):
    """Clear environment variable values in .env file, keeping them as empty."""
    if not env_file.exists():
        print(f"Error: .env file not found: {env_file}")
        return False

    content = env_file.read_text()
    original_content = content
    lines = content.split('\n')
    new_lines = []
    found_vars = set()

    # Pattern to match environment variable assignments
    # Matches: VAR_NAME=value, VAR_NAME="value", VAR_NAME='value', VAR_NAME= (empty)
    # Also handles commented lines: # VAR_NAME=value
    for line in lines:
        modified = False
        for var_name in vars_to_clear:
            # Match from start of line, optional whitespace, optional comment, var name, =, and rest
            pattern = rf'^(\s*)(#?\s*)({re.escape(var_name)}\s*=\s*)([^\n]*)'
            match = re.match(pattern, line)
            if match:
                # If line is commented, uncomment it and set to empty
                if match.group(2).strip().startswith('#'):
                    new_lines.append(f"{var_name}=")
                else:
                    # Keep original indentation and set value to empty
                    new_lines.append(f"{match.group(1)}{match.group(3)}")
                found_vars.add(var_name)
                modified = True
                break

        if not modified:
            new_lines.append(line)

    # Add any missing variables as empty at the end
    missing_vars = set(vars_to_clear) - found_vars
    if missing_vars:
        if new_lines and new_lines[-1]:
            new_lines.append("")
        new_lines.append("# Provisioned IDs (cleared)")
        for var_name in sorted(missing_vars):
            new_lines.append(f"{var_name}=")

    content = '\n'.join(new_lines)

    if content != original_content:
        # Create backup
        backup_path = env_file.with_suffix('.env.bak')
        if backup_path.exists():
            backup_path.unlink()  # Remove old backup
        env_file.rename(backup_path)
        print(f"✓ Created backup: {backup_path.name}")

        # Write updated content
        env_file.write_text(content)
        return True

    return False


def main():
    print("=" * 60)
    print("Clear Provisioned IDs from docker/.env")
    print("=" * 60)

    # Find docker/.env file
    script_dir = Path(__file__).parent
    repo_root = script_dir.parent.parent
    env_file = repo_root / "docker" / ".env"

    if not env_file.exists():
        print(f"Error: .env file not found: {env_file}")
        print("  Make sure you're running this from the repository root")
        sys.exit(1)

    print(f"\nClearing provisioned IDs from: {env_file}")
    print("\nVariables to clear:")
    for var in PROVISIONED_VARS:
        print(f"  - {var}")

    # Clear the variables
    if clear_env_vars(env_file, PROVISIONED_VARS):
        print(f"\n✓ Successfully cleared provisioned IDs from {env_file.name}")
        print(f"  Backup saved as: {env_file.name}.bak")
        print("  Variables are now set to empty (e.g., MANAGER_DOMAIN_ID=)")
        print("\nNote: You may need to recreate services after clearing these values.")
    else:
        print(f"\n⚠ No changes made to {env_file.name}")
        print("  (Variables may not exist or were already cleared)")

    print("\n" + "=" * 60)


if __name__ == "__main__":
    main()
