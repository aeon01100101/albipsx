#!/bin/bash
# Run every engine check.
cd "$(dirname "$0")"
exec /home/hatch/workspace/albish/bin/albish test_engine.alb
