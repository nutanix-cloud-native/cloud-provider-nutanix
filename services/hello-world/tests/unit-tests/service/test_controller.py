from service import controller

def test_health_check(mocker):
    mocker.patch("service.controller", "app")

    assert controller.hello_world() == '{"message": "hello-world"}'


