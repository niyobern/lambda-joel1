import requests
import json
from pydantic import BaseModel
from datetime import datetime
from typing import Dict, Any

class Transaction(BaseModel):
    ref: str
    status: str
    amount: int
    provider: str
    kind: str
    created_at: datetime

class AuthResponse(BaseModel):
    access: str
    refresh: str
    expires: int

class Transaction(BaseModel):
    ref: str
    amount: float
    fee: float
    kind: str
    provider: str
    client: str # phone number
    metadata: Dict[str, Any]
    merchant: str
    timestamp: datetime

class TransactionNotFound(BaseModel):
    message: str

app_id = "17eabc50-c552-11f0-9c11-deadd43720af"
app_secret = "9491032d457e34ef97a330564d321fd6da39a3ee5e6b4b0d3255bfef95601890afd80709"


def authorize() -> AuthResponse:
    url = "https://payments.paypack.rw/api/auth/agents/authorize"
    payload = json.dumps({
    "client_id": app_id,
    "client_secret": app_secret
    })
    headers = {
    'Content-Type': 'application/json',
    'Accept': 'application/json'
    }
    response = requests.request("POST", url, headers=headers, data=payload, timeout=50000)
    return response.json()

def cashin(number, amount) -> Transaction:
    auth = authorize()
    access_token = auth["access"]
    url = "https://payments.paypack.rw/api/transactions/cashin"
    payload = json.dumps({
    "amount": amount,
    "number": number
    })
    headers = {
    'Content-Type': 'application/json',
    'Accept': 'application/json',
    'Authorization': f'Bearer {access_token}'
    }
    response = requests.request("POST", url, headers=headers, data=payload, timeout=50000)
    return response.json()

def cashout(number, amount):
    auth = authorize()
    access_token = auth["access"]
    url = "https://payments.paypack.rw/api/transactions/cashout"
    payload = json.dumps({
    "amount": amount,
    "number": number
    })
    headers = {
    'Content-Type': 'application/json',
    'Accept': 'application/json',
    'Authorization': f'Bearer {access_token}'
    }
    response = requests.request("POST", url, headers=headers, data=payload, timeout=50000)
    return response.json()

def find_transaction(ref) -> Transaction | TransactionNotFound:
    auth = authorize()
    access_token = auth["access"]
    url = f"https://payments.paypack.rw/api/transactions/find/{ref}"
    payload = {}
    headers = {
    'Content-Type': 'application/json',
    'Accept': 'application/json',
    'Authorization': f'Bearer {access_token}'
    }
    response = requests.request("GET", url, headers=headers, data=payload, timeout=50000)
    return response.json()

test = find_transaction("dbed4dbb-f1bd-433d-ba57-e383c5faa96b")
print(test)