module Domain.Rating exposing (Rating(..), all, decoder, fromString, label, toString)

import Json.Decode as Decode exposing (Decoder)


type Rating
    = Unrated
    | Safe
    | Questionable
    | Explicit


all : List Rating
all =
    [ Unrated, Safe, Questionable, Explicit ]


toString : Rating -> String
toString rating =
    case rating of
        Unrated ->
            "unrated"

        Safe ->
            "safe"

        Questionable ->
            "questionable"

        Explicit ->
            "explicit"


label : Rating -> String
label rating =
    case rating of
        Unrated ->
            "Unrated"

        Safe ->
            "Safe"

        Questionable ->
            "Questionable"

        Explicit ->
            "Explicit"


fromString : String -> Maybe Rating
fromString value =
    case value of
        "unrated" ->
            Just Unrated

        "safe" ->
            Just Safe

        "questionable" ->
            Just Questionable

        "explicit" ->
            Just Explicit

        _ ->
            Nothing


decoder : Decoder Rating
decoder =
    Decode.string
        |> Decode.andThen
            (\value ->
                case fromString value of
                    Just rating ->
                        Decode.succeed rating

                    Nothing ->
                        Decode.fail ("unknown rating: " ++ value)
            )
