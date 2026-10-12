module ApiErrorTests exposing (suite)

import Api.Error as ApiError
import Http


caseClassifiesEveryHttpVariant : Bool
caseClassifiesEveryHttpVariant =
    ApiError.classify (Http.BadUrl "https://bad.invalid") == ApiError.BadUrl "https://bad.invalid"
        && ApiError.classify Http.Timeout == ApiError.Timeout
        && ApiError.classify Http.NetworkError == ApiError.NetworkError
        && ApiError.classify (Http.BadStatus 400) == ApiError.BadStatus 400
        && ApiError.classify (Http.BadStatus 404) == ApiError.BadStatus 404
        && ApiError.classify (Http.BadStatus 409) == ApiError.BadStatus 409
        && ApiError.classify (Http.BadStatus 500) == ApiError.BadStatus 500
        && ApiError.classify (Http.BadBody "invalid") == ApiError.BadBody


suite : List ( String, Bool )
suite =
    [ ( "shared HTTP classification preserves every variant and URL", caseClassifiesEveryHttpVariant )
    ]
