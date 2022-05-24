pilot-local --log_output_level=ads:debug |& hh -p=xds
proxy-local --proxyLogLevel="upstream:debug,config:debug" |& tee /tmp/log
while :; do curl localhost:8080/debug/adsz?push=true; done

Make take a couple attempts but should eventually get stuck
