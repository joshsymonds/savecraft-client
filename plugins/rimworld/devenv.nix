{ pkgs, ... }:

{
  packages = [
    pkgs.dotnetCorePackages.sdk_9_0
    pkgs.protobuf # protoc for C# codegen
  ];
}
