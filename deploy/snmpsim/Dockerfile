# SNMP simulator for development and tests only. Never part of a release.
FROM python:3.12-alpine
# snmpsim is maintained by LeXtudio; pinned to the version current when this
# was written (look it up with `pip index versions snmpsim` and pin exactly).
ARG SNMPSIM_VERSION=1.2.2
# Two runtime dependencies snmpsim 1.2.2 needs but does not declare:
#  - pysmi: imported unconditionally by snmpsim.utils; only listed under the
#    package's "dev" extra, so pip install snmpsim alone omits it and the
#    responder crashes at startup with ModuleNotFoundError.
#  - cryptography: pysnmp's USM privacy layer uses it for AES/DES; without it,
#    SNMPv3 authPriv requests fail server-side with "Ciphering services not
#    available or ciphertext is broken" (a decryption error, not a bad key).
RUN pip install --no-cache-dir "snmpsim==${SNMPSIM_VERSION}" pysmi cryptography
RUN adduser -D snmpsim
COPY data /usr/local/snmpsim/data
USER snmpsim
ENTRYPOINT ["snmpsim-command-responder", "--data-dir=/usr/local/snmpsim/data"]
