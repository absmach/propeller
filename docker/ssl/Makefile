# Copyright (c) Abstract Machines
# SPDX-License-Identifier: Apache-2.0

CRT_LOCATION = certs
O ?= Propeller
OU_CA ?= propeller_ca
OU_CRT ?= propeller_crt
EA ?= propeller@absmach.eu
CN_CA ?= Propeller_Self_Signed_CA
CN_SRV ?= localhost
CRT_FILE_NAME ?= proplet
# The CN is an identifier only (for example, an Atom entity UUID). Never put
# an MQTT password or API key in a certificate subject.
CLIENT_CN ?= $(CRT_FILE_NAME)
COAP_DTLS_SERVER_CONF_FILE_NAME=coap-server.conf
COAP_DTLS_SERVER_CN=coap
COAP_DTLS_SERVER_CRT_FILE_NAME=coap-server

define GRPC_CERT_CONFIG
[req]
req_extensions = v3_req
distinguished_name = dn
prompt = no

[dn]
CN = propeller.svc
C  = RS
ST = RS
L  = BELGRADE
O  = Propeller
OU = Propeller

[v3_req]
subjectAltName = @alt_names

[alt_names]
DNS.1 = <<SERVICE_NAME>>
endef

define ANNOUNCE_BODY
Version $(VERSION) of $(PACKAGE_NAME) has been released.

It can be downloaded from $(DOWNLOAD_URL).

etc, etc.
endef
all: clean_certs ca server_cert client_cert coap_dtls_certs

$(CRT_LOCATION):
	mkdir -p $@

ca server_cert client_cert: | $(CRT_LOCATION)

# CA name and key is "ca".
ca:
	openssl req -newkey rsa:2048 -x509 -nodes -sha512 -days 1095 \
				-keyout $(CRT_LOCATION)/ca.key -out $(CRT_LOCATION)/ca.crt -subj "/CN=$(CN_CA)/O=$(O)/OU=$(OU_CA)/emailAddress=$(EA)"

# Server cert and key name is "propeller-server".
server_cert:
	# Create propeller server key and CSR.
	openssl req -new -sha256 -newkey rsa:4096 -nodes -keyout $(CRT_LOCATION)/propeller-server.key \
				-out $(CRT_LOCATION)/propeller-server.csr -subj "/CN=$(CN_SRV)/O=$(O)/OU=$(OU_CRT)/emailAddress=$(EA)"

	# Sign server CSR with SANs for container hostnames.
	printf '[v3_req]\nsubjectAltName=DNS:localhost,DNS:nginx,DNS:%s' "$(CN_SRV)" > $(CRT_LOCATION)/propeller-server.san
	openssl x509 -req -days 1000 -in $(CRT_LOCATION)/propeller-server.csr \
		-CA $(CRT_LOCATION)/ca.crt -CAkey $(CRT_LOCATION)/ca.key -CAcreateserial \
		-out $(CRT_LOCATION)/propeller-server.crt \
		-extfile $(CRT_LOCATION)/propeller-server.san -extensions v3_req

	# Remove CSR and SAN config.
	rm $(CRT_LOCATION)/propeller-server.csr $(CRT_LOCATION)/propeller-server.san

client_cert:
	# Create propeller client key and CSR.
	openssl req -new -sha256 -newkey rsa:4096 -nodes -keyout $(CRT_LOCATION)/$(CRT_FILE_NAME).key \
				-out $(CRT_LOCATION)/$(CRT_FILE_NAME).csr -subj "/CN=$(CLIENT_CN)/O=$(O)/OU=$(OU_CRT)/emailAddress=$(EA)"

	# Sign an explicit X.509 v3 client certificate. Rustls rejects v1 leaf
	# certificates, even when OpenSSL and Go accept them.
	printf '[v3_client]\nbasicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature,keyEncipherment\nextendedKeyUsage=clientAuth\nsubjectKeyIdentifier=hash\nauthorityKeyIdentifier=keyid,issuer\n' > $(CRT_LOCATION)/$(CRT_FILE_NAME)-client.conf
	openssl x509 -req -days 730 -in $(CRT_LOCATION)/$(CRT_FILE_NAME).csr -CA $(CRT_LOCATION)/ca.crt -CAkey $(CRT_LOCATION)/ca.key -CAcreateserial -out $(CRT_LOCATION)/$(CRT_FILE_NAME).crt \
		-extfile $(CRT_LOCATION)/$(CRT_FILE_NAME)-client.conf -extensions v3_client

	# Remove CSR and extension config.
	rm $(CRT_LOCATION)/$(CRT_FILE_NAME).csr $(CRT_LOCATION)/$(CRT_FILE_NAME)-client.conf

# Function to generate gRPC certificates (server or client)
# Usage: $(call gen_grpc_cert,cert_file_name,common_name)
clean_certs:
	rm -rf $(CRT_LOCATION)/*.crt $(CRT_LOCATION)/*.key $(CRT_LOCATION)/*.srl $(CRT_LOCATION)/*.conf
