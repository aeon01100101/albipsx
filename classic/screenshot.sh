#!/bin/bash
# Paint one picture of the game: ../output/frame.ppm and ../output/frame.png
cd "$(dirname "$0")"
mkdir -p ../output
/home/hatch/workspace/albish/bin/albish screenshot.alb > ../output/frame.ppm && python3 ../tools/ppm2png.py ../output/frame.ppm ../output/frame.png
