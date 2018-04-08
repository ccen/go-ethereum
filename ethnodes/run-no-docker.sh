#!/bin/bash
# check if root
if [ "$EUID" -ne 0 ]; then
  echo "please run as root"
  exit 1
fi
if [ "$#" -ne 1 ]; then
  echo "argument missing"
  echo "usage: sudo ./run-no-docker.sh scale-size"
  exit 1
fi

n=$(( $1 - 1 ))
WORKING_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd ${WORKING_DIR}
MYSQL_NAME="ethnodes-mysql"

# build mysql container image
MYSQL_IMAGE="mysql:5.7-ethnodes"
docker build -t ${MYSQL_IMAGE} -f ethnodes-dockerfile .

# get env variables
source .env

# run mysql containers
MYSQL_PORT=3300
echo "starting mysql containers..."
for i in `seq 0 ${n}`;
do
  MYSQL_DIR="${ROOT_DIR}-${i}/ethnodes"
  BACKUP_DIR="${ROOT_DIR}-${i}/ethnodes-backup"
  mkdir -p ${BACKUP_DIR}
  chmod 777 ${BACKUP_DIR}
  PORT=$(( ${MYSQL_PORT}+${i} ))
  docker run -d --restart=always -p ${PORT}:3306 -h ${MYSQL_NAME}-${i} --name ${MYSQL_NAME}-${i} \
    --env MYSQL_DATABASE=${MYSQL_DB} \
    --env MYSQL_ROOT_PASSWORD=${MYSQL_PASSWORD} \
    --env MYSQL_USER=${MYSQL_USERNAME} \
    --env MYSQL_PASSWORD=${MYSQL_PASSWORD} \
    -v ${WORKING_DIR}/configs:/etc/mysql/conf.d \
    -v ${MYSQL_DIR}:/var/lib/mysql \
    -v ${BACKUP_DIR}:/backup \
    ${MYSQL_IMAGE}
  echo "${MYSQL_NAME}-${i} started"
done

read -p "Press enter to continue... "
cd ${WORKING_DIR}/..
make geth
cp ${WORKING_DIR}/../build/bin/geth /usr/bin/geth

# run node-finders
for i in `seq 0 ${n}`;
do
  ${WORKING_DIR}/node-finder-loop.sh ${i} > ${ROOT_DIR}-${i}/node-finder-loop.log 2>&1 &
  echo "node-finder-${i} loop started"
done