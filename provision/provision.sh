#!/bin/bash

set -e

cd ..

echo "Starting Supermq..."
make start-supermq

if [ $? -ne 0 ]; then
    echo "Failed to start Supermq"
    exit 1
fi

cd provision

echo "Cloning Supermq repository..."
git clone https://github.com/absmach/supermq.git
cd supermq

echo "Building CLI tool..."
make cli

if [ $? -ne 0 ]; then
    echo "Failed to build CLI tool"
    exit 1
fi

echo "Getting user token..."
USER_TOKEN=$(./build/cli users token admin@example.com 12345678 | jq -r .access_token)

if [ -z "$USER_TOKEN" ]; then
    echo "Failed to get user token"
    exit 1
fi

echo "Creating domain..."
DOMAIN_ID=$(./build/cli domains create demo demo $USER_TOKEN | jq -r .id)

if [ -z "$DOMAIN_ID" ]; then
    echo "Failed to create domain"
    exit 1
fi

echo "Creating manager thing..."
MANAGER_RESPONSE=$(./build/cli things create '{"name": "Propeller Manager", "tags": ["manager", "propeller"], "status": "enabled"}' $DOMAIN_ID $USER_TOKEN)

export MANAGER_THING_ID=$(echo $MANAGER_RESPONSE | jq -r .id)
export MANAGER_THING_KEY=$(echo $MANAGER_RESPONSE | jq -r .credentials.secret)

if [ -z "$MANAGER_THING_ID" ] || [ -z "$MANAGER_THING_KEY" ]; then
    echo "Failed to create manager thing or parse its credentials"
    exit 1
fi

echo "Creating channel..."
CHANNEL_RESPONSE=$(./build/cli channels create '{"name": "Propeller Manager", "tags": ["manager", "propeller"], "status": "enabled"}' $DOMAIN_ID $USER_TOKEN)

export MANAGER_CHANNEL_ID=$(echo $CHANNEL_RESPONSE | jq -r .id)
export PROPLET_CHANNEL_ID=$MANAGER_CHANNEL_ID

if [ -z "$MANAGER_CHANNEL_ID" ]; then
    echo "Failed to create channel"
    exit 1
fi

echo "Creating proplet thing..."
PROPLET_RESPONSE=$(./build/cli things create '{"name": "Propeller Proplet", "tags": ["proplet", "propeller"], "status": "enabled"}' $DOMAIN_ID $USER_TOKEN)

export PROPLET_THING_ID=$(echo $PROPLET_RESPONSE | jq -r .id)
export PROPLET_THING_KEY=$(echo $PROPLET_RESPONSE | jq -r .credentials.secret)

if [ -z "$PROPLET_THING_ID" ] || [ -z "$PROPLET_THING_KEY" ]; then
    echo "Failed to create proplet thing or parse its credentials"
    exit 1
fi

echo "Connecting manager thing to channel..."
./build/cli things connect $MANAGER_THING_ID $MANAGER_CHANNEL_ID $DOMAIN_ID $USER_TOKEN

echo "Connecting proplet thing to channel..."
./build/cli things connect $PROPLET_THING_ID $PROPLET_CHANNEL_ID $DOMAIN_ID $USER_TOKEN

echo "Setup completed successfully!"

echo "Exported variables:"
echo "MANAGER_THING_ID=$MANAGER_THING_ID"
echo "MANAGER_THING_KEY=$MANAGER_THING_KEY"
echo "MANAGER_CHANNEL_ID=$MANAGER_CHANNEL_ID"
echo "PROPLET_CHANNEL_ID=$PROPLET_CHANNEL_ID"
echo "PROPLET_THING_ID=$PROPLET_THING_ID"
echo "PROPLET_THING_KEY=$PROPLET_THING_KEY"