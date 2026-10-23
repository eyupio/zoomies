#!/usr/bin/env python3
"""Point eyup.io at GitHub Pages through the Cloudflare API.

The token is read from CF_API_TOKEN. Set APPLY=true to change records; the
default is a dry run that prints the current records and the plan.
"""

import json
import os
import sys
import urllib.error
import urllib.request

API = "https://api.cloudflare.com/client/v4"
ZONE_NAME = "eyup.io"
APPLY = os.environ.get("APPLY", "false").lower() in {"1", "true", "yes"}
TOKEN = os.environ.get("CF_API_TOKEN", "")

APEX_A = [
    "185.199.108.153",
    "185.199.109.153",
    "185.199.110.153",
    "185.199.111.153",
]
APEX_AAAA = [
    "2606:50c0:8000::153",
    "2606:50c0:8001::153",
    "2606:50c0:8002::153",
    "2606:50c0:8003::153",
]
WWW_TARGET = "eyupio.github.io"


def api(method, path, body=None):
    data = json.dumps(body).encode("utf-8") if body is not None else None
    request = urllib.request.Request(API + path, data=data, method=method)
    request.add_header("Authorization", "Bearer " + TOKEN)
    request.add_header("Content-Type", "application/json")
    request.add_header("User-Agent", "eyup-dns/1.0")
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            payload = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as error:
        raw = error.read().decode("utf-8", errors="replace")
        try:
            payload = json.loads(raw)
        except json.JSONDecodeError:
            payload = {"errors": [{"message": raw[:400]}]}
        print(f"{method} {path} returned HTTP {error.code}", file=sys.stderr)
        print(json.dumps(payload.get("errors", payload), indent=2), file=sys.stderr)
        raise SystemExit(1)
    if not payload.get("success"):
        print(f"{method} {path} returned an API error", file=sys.stderr)
        print(json.dumps(payload.get("errors", payload), indent=2), file=sys.stderr)
        raise SystemExit(1)
    return payload.get("result")


def main():
    if not TOKEN:
        print("CF_API_TOKEN is empty", file=sys.stderr)
        raise SystemExit(2)

    zones = api("GET", f"/zones?name={ZONE_NAME}")
    if not zones:
        print(f"No Cloudflare zone named {ZONE_NAME}", file=sys.stderr)
        raise SystemExit(1)
    zone = zones[0]
    zone_id = zone["id"]
    print(f"Cloudflare zone: {zone['name']} ({zone_id})")

    records = []
    page = 1
    while True:
        batch = api(
            "GET",
            f"/zones/{zone_id}/dns_records?per_page=100&page={page}",
        )
        records.extend(batch)
        if len(batch) < 100:
            break
        page += 1

    managed_names = {ZONE_NAME, f"www.{ZONE_NAME}"}
    managed_types = {"A", "AAAA", "CNAME"}
    managed = [
        record
        for record in records
        if record["type"] in managed_types and record["name"] in managed_names
    ]

    print("\nExisting web records:")
    if managed:
        for record in managed:
            print(
                f"  {record['type']:6} {record['name']:18} "
                f"{record['content']:42} proxied={record['proxied']} ttl={record['ttl']}"
            )
    else:
        print("  (none)")

    counts = {}
    for record in records:
        if record not in managed:
            counts[record["type"]] = counts.get(record["type"], 0) + 1
    if counts:
        print("\nOther record types left alone:")
        print("  " + ", ".join(f"{kind}({count})" for kind, count in sorted(counts.items())))

    desired = {}
    for address in APEX_A:
        desired[("A", ZONE_NAME, address)] = {
            "type": "A",
            "name": ZONE_NAME,
            "content": address,
            "ttl": 1,
            "proxied": False,
        }
    for address in APEX_AAAA:
        desired[("AAAA", ZONE_NAME, address)] = {
            "type": "AAAA",
            "name": ZONE_NAME,
            "content": address,
            "ttl": 1,
            "proxied": False,
        }
    desired[("CNAME", f"www.{ZONE_NAME}", WWW_TARGET)] = {
        "type": "CNAME",
        "name": f"www.{ZONE_NAME}",
        "content": WWW_TARGET,
        "ttl": 1,
        "proxied": False,
    }

    seen = set()
    to_delete = []
    to_update = []
    for record in managed:
        key = (record["type"], record["name"], record["content"])
        if key in desired and key not in seen:
            seen.add(key)
            if record["proxied"] or record["ttl"] != 1:
                to_update.append(record)
        else:
            to_delete.append(record)
    to_add = [spec for key, spec in desired.items() if key not in seen]

    print("\nPlan:")
    if not (to_delete or to_update or to_add):
        print("  (already in the desired state)")
    for record in to_delete:
        print(f"  delete {record['type']:6} {record['name']:18} {record['content']}")
    for record in to_update:
        print(
            f"  update {record['type']:6} {record['name']:18} {record['content']} "
            "-> proxied=false, ttl=1"
        )
    for spec in to_add:
        print(f"  add    {spec['type']:6} {spec['name']:18} {spec['content']}")

    if not APPLY:
        print("\nDry run only. Re-run with apply=true to apply.")
        return

    for record in to_delete:
        api("DELETE", f"/zones/{zone_id}/dns_records/{record['id']}")
        print(f"deleted {record['type']} {record['name']} {record['content']}")
    for record in to_update:
        api(
            "PATCH",
            f"/zones/{zone_id}/dns_records/{record['id']}",
            {"proxied": False, "ttl": 1},
        )
        print(f"updated {record['type']} {record['name']} {record['content']}")
    for spec in to_add:
        api("POST", f"/zones/{zone_id}/dns_records", spec)
        print(f"added {spec['type']} {spec['name']} {spec['content']}")

    final = api(
        "GET",
        f"/zones/{zone_id}/dns_records?per_page=100&page=1",
    )
    final_managed = [
        record
        for record in final
        if record["type"] in managed_types and record["name"] in managed_names
    ]
    print("\nFinal web records:")
    for record in final_managed:
        print(
            f"  {record['type']:6} {record['name']:18} "
            f"{record['content']:42} proxied={record['proxied']} ttl={record['ttl']}"
        )


if __name__ == "__main__":
    main()
