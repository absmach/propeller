"""
Train a simple linear regression model for the ML-WASM example.

This script generates mymodel.pkl which contains a trained sklearn LinearRegression model.
The model coefficients are then extracted and used in model_gen.go to create a pure Go
implementation that can be compiled to WASM.

The model learns: y = 2*x0 + 3*x1 (approximately, due to training data)

Usage:
    python3 train_model.py

Output:
    mymodel.pkl - Pickled sklearn model (used for coefficient extraction)
"""

from sklearn.linear_model import LinearRegression
import numpy as np
import os
import pickle

script_dir = os.path.dirname(os.path.abspath(__file__))

model_path = os.path.join(script_dir, "mymodel.pkl")

# Fake training data: y = 2*x1 + 3*x2
X = np.array([
    [0, 0],
    [1, 0],
    [0, 1],
    [1, 1],
    [2, 1],
    [1, 2],
], dtype=float)

y = 2 * X[:, 0] + 3 * X[:, 1]

model = LinearRegression()
model.fit(X, y)

with open(model_path, "wb") as f:
    pickle.dump(model, f)

print(f"Saved model to {model_path}")
