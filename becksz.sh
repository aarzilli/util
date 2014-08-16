#!/bin/sh

DIR=$1

if [ "x$DIR" = "x" ]; then
	echo "Usage: becksz.sh <backups directory>" >2
	exit 1
fi

find $DIR -type f | while read line
do
	stat -c "%i %s %n" "$line"
done | sort -n > becksz_part1_out

