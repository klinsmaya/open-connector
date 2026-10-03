#!/usr/bin/env python3
"""Compare actual pinned old/new SDK wire requests against a loopback fixture.
No checkout, network download, external request, or repository mutation occurs.
"""
import argparse
import hashlib
import os
from pathlib import Path
import subprocess
import tempfile

parser = argparse.ArgumentParser()
parser.add_argument("--multica", required=True, type=Path)
parser.add_argument("--go", required=True)
parser.add_argument("--candidate", required=True)
args = parser.parse_args()
base = "a9e82c79739446111b8ca9acbb256f584072d20d"
probe = Path(__file__).resolve().parents[1] / "tests/baseline/official_wire_probe.go.txt"
def git(*parts):
    return subprocess.check_output(["git", "-C", str(args.multica), *parts])
head = git("rev-parse", args.candidate + "^{commit}").decode().strip()
outputs = []
with tempfile.TemporaryDirectory(prefix="oc-official-wire-") as root:
    for index, revision in enumerate([base, head]):
        folder = Path(root) / str(index)
        folder.mkdir()
        names = git("ls-tree", "-r", "--name-only", revision, "server/pkg/composio", "server/go.mod", "server/go.sum").decode().splitlines()
        for name in names:
            if name.endswith("_test.go") or not (name.endswith(".go") or name in ("server/go.mod", "server/go.sum")):
                continue
            path = folder / name.removeprefix("server/")
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(git("show", revision + ":" + name))
        (folder / "pkg/composio/baseline_wire_test.go").write_bytes(probe.read_bytes())
        output = folder / "wire.json"
        env = dict(os.environ, GOWORK="off", GOPROXY="off", GOSUMDB="off", GOTOOLCHAIN="local", OFFICIAL_WIRE_OUTPUT=str(output))
        subprocess.run([args.go, "test", "./pkg/composio", "-run", "^TestOfficialBaselineWireProbe$", "-count=1"], cwd=folder, env=env, check=True)
        outputs.append(output.read_bytes())
    if outputs[0] != outputs[1]:
        raise SystemExit("FAIL: official baseline wire differs; inspect the pinned SDK changes")
    print("PASS official wire: 7 endpoints and MCP headers identical")
    print("baseline=" + base + " candidate=" + head)
    print("normalized_wire_sha256=" + hashlib.sha256(outputs[0]).hexdigest())
