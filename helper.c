#define _GNU_SOURCE
#include <sched.h>
#include <stdio.h>
#include <stdlib.h>
#include <fcntl.h>
#include <unistd.h>
#include <sys/mount.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <string.h>
#include <errno.h>

void enter_namespace(pid_t target_pid, const char *ns_type) {
    char ns_path[256];
    snprintf(ns_path, sizeof(ns_path), "/proc/%d/ns/%s", target_pid, ns_type);
    int fd = open(ns_path, O_RDONLY);
    if (fd == -1) {
        perror("open namespace");
        exit(EXIT_FAILURE);
    }
    if (setns(fd, 0) == -1) {
        perror("setns");
        close(fd);
        exit(EXIT_FAILURE);
    }
    close(fd);
}

int main(int argc, char *argv[]) {
    if (argc < 4) {
        fprintf(stderr, "Usage: %s <PID> <rootfs> <command> [args...]\n", argv[0]);
        exit(EXIT_FAILURE);
    }

    pid_t target_pid = atoi(argv[1]);
    const char *rootfs = argv[2];

    // Enter namespaces
    const char *namespaces[] = {"pid", "uts", "mnt", "net"};
    for (int i = 0; i < 4; i++) {
        enter_namespace(target_pid, namespaces[i]);
    }

    // Make sure the mount point exists
    struct stat st = {0};
    if (stat(rootfs, &st) == -1) {
        perror("stat rootfs");
        exit(EXIT_FAILURE);
    }

    // Change root to the new rootfs
    if (chdir(rootfs) != 0) {
        perror("chdir to rootfs");
        exit(EXIT_FAILURE);
    }

    if (chroot(".") != 0) {
        perror("chroot");
        exit(EXIT_FAILURE);
    }

    if (chdir("/") != 0) {
        perror("chdir to /");
        exit(EXIT_FAILURE);
    }

    // Execute the command
    execvp(argv[3], &argv[3]);
    perror("execvp");
    exit(EXIT_FAILURE);
}
