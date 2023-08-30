ARG container_img
FROM ${container_img}
ARG version
ARG enable_local_repo
ARG mariner_repo
LABEL containerized-rpmbuild=$mariner_repo/build

COPY resources/local_repo /etc/yum.repos.d/local_repo.disabled_repo

RUN echo "alias tdnf='tdnf --releasever=$version'"               >> /root/.bashrc && \
    echo "source /mariner_setup_dir/setup_functions.sh"          >> /root/.bashrc && \
    echo "if [[ ! -L /repo ]]; then ln -s /mnt/RPMS/ /repo; fi"  >> /root/.bashrc

#if enable_local_repo is set to true
RUN if [[ "${enable_local_repo}" == "true" ]]; then echo "enable_local_repo" >> /root/.bashrc; fi

RUN echo "cat /mariner_setup_dir/splash.txt"                     >> /root/.bashrc && \
    echo "show_help"                                             >> /root/.bashrc && \
    echo "cd /usr/src/mariner/ || { echo \"ERROR: Could not change directory to /usr/src/mariner/ \"; exit 1; }"  >> /root/.bashrc

# Install vim & git in the build env
RUN tdnf --releasever=$version install -y vim git
