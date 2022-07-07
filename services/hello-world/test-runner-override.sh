#!/bin/bash
current_dir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"

pip install --upgrade pip

pip -V

if [ -e "${current_dir}/tests/test-requirements.txt" ]; then
  echo "Installing testing requirements..."
  export PIP_CERT=`python -c "import certifi; print(certifi.where())"`
  pip install -r ${current_dir}/tests/test-requirements.txt
fi

cd ${current_dir}
echo "Running pytest"
pytest --cov code --cov-report html:coverage --junitxml=test-results/junit.xml tests || exit 1

