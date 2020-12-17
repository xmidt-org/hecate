# Deploying Hecate

In this readme, we'll walk through deploying a subset of the XMiDT cluster using [docker-compose](https://docs.docker.com/install). We'll do this so we understand how to perform the live migration of webhook backends from SNS to Argus. Since Caduceus and Tr1d1um are the main existing services relevant to webhooks (besides the obvious dependencies), we'll keep it simple and only include these to our sample cluster.

## The scenario

Suppose we have a XMiDT cluster in which Tr1d1um servers are inserting webhooks to SNS and Caduceus machines are fetching them (both leveraging SNS). Our goal is for the cluster to continue normal operation during the update of services (in this process, different versions of the same service will rely on different backends). Although there are multiple ways to proceed with the migration, we recommend the following:

- **Deploy cluster of hecate machines:** ensure that all the SNS webhook updates are being pushed to Argus so that the new Caduceus machines will function properly.
- **Perform a rolling update of Caduceus servers:** if hecate is working as expected, there should not be surprises here. By the end of this process, XMiDT should no longer be reading updates from SNS directly (only through Hecate).
- **Perform a rolling update of Tr1d1um servers:** during this process, webhook writes will come from both SNS-Tr1d1um and Argus-Tr1d1um servers. By the end of this, we should be fully transitioned into a cluster relying only on Argus for webhooks.

## A simple Hecate cluster

In this docker-compose cluster, we will have:

- Caduceus: one server listening for webhook updates from Argus.
- Tr1d1um: one server writing webhook registrations to SNS.
- Hecate
- Argus
- SNS
- Prometheus: one server providing easy access to metrics to verify synchronization is happening.

### Deploy

```bash
# Build hecate image
cd ${HECATE_REPO_DIR}
make local-docker

# Stand up docker-compose cluster
cd deploy/docker-compose
./deploy.sh
```

### Insert a webhook through Tr1d1um

```bash
curl --location --request POST 'http://localhost:6100/api/v2/hook' \
--header 'Authorization: Basic dXNlcjpwYXNz' \
--header 'Content-Type: application/json' \
--data-raw '[{
  "config": {
    "url": "http://hecate-cluster-test/hook/ingest/0",
    "content_type": "application/json",
    "secret": "secretString"
  },
  "matcher": {
    "device_id": [
      "dontMatchMe"
    ]
  },
  "events": [
   "dontMatchMe"
  ],
  "duration": 3000,
  "address": "http://hecate-test-cluster.net"
}]'
```

### Verify corresponding item exists in Argus

```bash
curl --location --request GET 'http://localhost:6600/api/v1/store/webhooks' \
--header 'Authorization: Basic dXNlcjpwYXNz' \
--header 'X-Midt-Owner: Argus' \
--header 'Content-Type: application/json'
```

### Verify list size of webhooks in Caduceus

Visit http://localhost:9090/graph?g0.expr=webpa_tr1d1um_webhook_list_size_value&g0.tab=1&g0.stacked=0&g0.range_input=1h&g1.expr=xmidt_caduceus_webhook_list_size_value&g1.tab=1&g1.stacked=0&g1.range_input=1h

to verify that the webhook list size matches between Caduceus and Tr1d1um.
