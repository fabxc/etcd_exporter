FROM       golang:1.4
MAINTAINER Fabian Reinartz <fab.reinartz@gmail.com>

EXPOSE     9105
ENTRYPOINT ["go-wrapper", "run"]