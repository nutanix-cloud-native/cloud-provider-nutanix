import json
import logging
import sys

import bottle

app = bottle.Bottle()
logger = logging.getLogger(__name__)

@app.route('/hello-world', method='GET')
def hello_world():
    ''' return hello-world text message back '''
    bottle.response.status = 200
    return json.dumps({
        'message': 'hello-world'
    })

@app.route('/health_check', method='GET')
def health_check():
    ''' return 200 for health check and the service status'''
    bottle.response.status = 200
    return json.dumps({
        'service_name' : 'hello-world',
        'status': 'ok'
    })

def main(argv):
    ''' main method '''
    port = 80
    # allow user to override default port
    if len(argv) == 1:
        port = int(argv[0])
    logger.debug('Hello world service running on port %i', port)
    app.run(host='0.0.0.0', port=port)

if __name__ == '__main__':
    main(sys.argv[1:])
