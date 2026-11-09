export function mediaRuntimeConfiguration(environment:Record<string,string|undefined>=process.env) {
  const standalone=environment.BACKEND_MODE==="standalone";
  const hostname=environment.PODCAST_BIND_HOST??(standalone?"127.0.0.1":"0.0.0.0");
  if(standalone&&hostname!=="127.0.0.1")throw new Error("Standalone podcast processor must bind to 127.0.0.1");
  const port=Number(environment.PODCAST_MEDIA_PORT??(standalone?"9080":environment.PORT??"8080"));
  if(!Number.isInteger(port)||port<1||port>65535)throw new Error("Invalid podcast processor port");
  const sqlitePath=standalone?environment.SQLITE_DB_PATH:undefined;
  if(standalone&&(!sqlitePath||!sqlitePath.startsWith("/")||sqlitePath.includes("\0")))throw new Error("Standalone podcast processor requires an absolute SQLITE_DB_PATH");
  return {standalone,hostname,port,sqlitePath};
}
