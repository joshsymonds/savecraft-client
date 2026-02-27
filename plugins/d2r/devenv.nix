{ pkgs, ... }:

{
  packages = [
    # CASCExtractor build deps
    pkgs.cmake
    pkgs.gcc
    pkgs.zlib
  ];
}
