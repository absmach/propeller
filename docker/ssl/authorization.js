// Copyright (c) Abstract Machines
// SPDX-License-Identifier: Apache-2.0

var clientKey = "";

// Check certificate HTTPS and WSS.
function setKey(r) {
  if (clientKey === "") {
    clientKey = parseCert(r.variables.ssl_client_s_dn, "CN");
  }

  var auth = r.headersIn["Authorization"];
  if (auth && auth.length && auth != clientKey) {
    r.error("Authorization header does not match certificate");
    return "";
  }

  if (r.uri.startsWith("/ws") && (!auth || !auth.length)) {
    var a;
    for (a in r.args) {
      if (a == "authorization" && r.args[a] === clientKey) {
        return clientKey;
      }
    }

    r.error("Authorization param does not match certificate");
    return "";
  }

  return clientKey;
}

function parseCert(cert, key) {
  if (cert.length) {
    var pairs = cert.split(",");
    for (var i = 0; i < pairs.length; i++) {
      var pair = pairs[i].split("=");
      if (pair[0].toUpperCase() == key) {
        return "Client " + pair[1].replace("\\", "").trim();
      }
    }
  }

  return "";
}

export default { setKey };
