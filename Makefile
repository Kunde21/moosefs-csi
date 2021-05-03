all:
	docker build -t moosefs-csi --build-arg MOOSEFS_IMAGE=moosefs/client --build-arg MOOSEFS_TAG=latest --file docker/Dockerfile .