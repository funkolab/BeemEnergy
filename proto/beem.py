import paho.mqtt.client as mqtt
import logging
import requests
import jwt
import time

# Remplacez ces valeurs par vos identifiants Beem Energy
email=''
password=''
# Si vous avez déjà un token d'accès, vous pouvez le définir ici , seulement pour le debug
token=""

if token != "":
    # Renouveler le token d'accès
    response = requests.get("https://api-x.beem.energy/beemapp/user/refresh", headers={'Authorization':'Bearer '+token})
    if response.status_code != 200:
        print('Failed to refresh access token, status code ', response.status_code)
        exit(response.status_code)
else:
    # Récupérer le token d'accès
    response = requests.post("https://api-x.beem.energy/beemapp/user/login", data={'email':email,'password':password})
    if response.status_code != 201:
        print('Failed to get REST token, status code ', response.status_code)
        exit(response.status_code)

token_rest = response.json().get('accessToken')
user_id = response.json().get('userId')
print('Token REST: ', token_rest)
client_id='beemapp-{0}-{1}'.format(user_id, round(time.time() * 1000))
print('Client ID: ', client_id)

# # Récupérer le token MQTT
response = requests.post("https://api-x.beem.energy/beemapp/devices/mqtt/token", headers={'Authorization':'Bearer '+token_rest}, data={'clientId': client_id, 'clientType': 'user'})
if response.status_code != 200:
    print('Failed to get MQTT token, status code ', response.status_code)
    exit(response.status_code)

token_mqtt = response.json().get('jwt')
print('Token MQTT: ', token_mqtt)

# Récupérer les numéros de série des appareils
response = requests.get("https://api-x.beem.energy/beemapp/devices", headers={'Authorization':'Bearer '+token_rest}, data={'clientId': client_id, 'clientType': 'user'})
serial_number=response.json()["energySwitches"][0]["serialNumber"]
print('Serial number: ', serial_number)


# Définir les détails du serveur MQTT
mqtt_server = "mqtt.beem.energy"
mqtt_port = 8883
# mqtt_topic = "battery/" + serial_number + "/sys/streaming"
mqtt_topic = "brain/" + serial_number + "/#"

# Définir le client MQTT
client = mqtt.Client(mqtt.CallbackAPIVersion.VERSION2, client_id)

# Activer les logs
logging.basicConfig(level=logging.DEBUG)
logger = logging.getLogger(__name__)
client.enable_logger(logger)

# Définir le nom d'utilisateur et le mot de passe pour le client (token JWT comme mot de passe)
client.username_pw_set(username=client_id, password=token_mqtt)

# Définir les fonctions de rappel
def on_connect(client, userdata, flags, rc, prop):
    print("Connecté avec le code de résultat " + str(rc))
    client.subscribe(mqtt_topic)

def on_connect_fail(client, userdata):
    print("Connection failed! ")

def on_message(client, userdata, msg):
    print("Topic: " + msg.topic + ", Message: " + str(msg.payload))

# Assigner les fonctions de rappel au client
client.on_connect = on_connect
client.on_message = on_message

# Se connecter au serveur MQTT avec SSL/TLS
client.tls_set()
client.connect(mqtt_server, mqtt_port, 60)

# Démarrer la boucle du client MQTT
client.loop_forever()