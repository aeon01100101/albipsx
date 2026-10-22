#!/bin/bash
# Draw 12 spinning pictures and print the RAM budget.
cd "$(dirname "$0")"
exec /home/hatch/workspace/albish/bin/albish bench.alb
