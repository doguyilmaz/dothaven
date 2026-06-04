#!/usr/bin/env bun
if (typeof Bun === "undefined") {
  console.error("dothaven requires Bun runtime. Install: https://bun.sh");
  process.exit(1);
}
import "../src/cli";
