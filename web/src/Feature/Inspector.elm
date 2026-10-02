module Feature.Inspector exposing (Config, view)

import Api.Post
import Domain.Post exposing (PostDetail, PostSummary)
import Feature.Selection
import Html exposing (Html, a, aside, button, dd, div, dl, dt, li, p, text, ul)
import Html.Attributes exposing (attribute, class, href, rel, style, target, title, type_)
import Html.Events exposing (on)
import Json.Decode as Decode
import Set exposing (Set)
import Ui.Media
import Ui.Panel as Panel exposing (Presentation)


type alias Config msg =
    { apiBase : String
    , presentation : Presentation
    , active : Maybe PostSummary
    , detail : Maybe PostDetail
    , detailLoading : Bool
    , detailError : Maybe String
    , selectedCount : Int
    , commonTags : List String
    , missing : Set String
    , onTag : String -> Bool -> msg
    , onClear : msg
    , onClose : msg
    , onApiPending : String -> msg
    , onMediaError : String -> msg
    }


view : Config msg -> Html msg
view config =
    aside [ class (Panel.presentationClass "inspector" config.presentation), attribute "aria-label" "Inspector" ]
        [ Panel.header "Inspector" config.presentation config.onClose
        , div [ class "panel-body" ]
            (case config.active of
                Nothing ->
                    [ p [ class "panel-note" ] [ text "No active post. Click a thumbnail or use the arrow keys." ]
                    , selectionSection config
                    ]

                Just post ->
                    [ identitySection config post
                    , selectionSection config
                    , detailSection config post
                    , Panel.section "Tags" (tagList config (detailTags config post))
                    , Panel.section "History"
                        [ p [ class "panel-note" ] [ text "Revision history arrives with the post detail API." ] ]
                    ]
            )
        ]


detailSection : Config msg -> PostSummary -> Html msg
detailSection config post =
    case config.detail of
        Just detail ->
            if detail.id == post.id then
                Panel.section "Source"
                    [ dl [ class "facts" ]
                        [ fact "Source" (if detail.source == "" then "—" else detail.source)
                        , fact "Artist" (if detail.artist == "" then "—" else detail.artist)
                        , fact "Hash" (if detail.hash == "" then "—" else detail.hash)
                        , fact "File size" (if detail.fileSize <= 0 then "—" else String.fromInt detail.fileSize ++ " bytes")
                        , fact "Created" (if detail.createdAt == "" then "—" else detail.createdAt)
                        ]
                    ]

            else
                detailPlaceholder config

        Nothing ->
            detailPlaceholder config


detailPlaceholder : Config msg -> Html msg
detailPlaceholder config =
    Panel.section "Source"
        (if config.detailLoading then
            [ p [ class "panel-note" ] [ text "Loading post details…" ] ]

         else
            case config.detailError of
                Just message ->
                    [ p [ class "panel-note status-error" ] [ text ("Post details unavailable: " ++ message) ] ]

                Nothing ->
                    [ p [ class "panel-note" ] [ text "Select a post to load its details." ] ]
        )


detailTags : Config msg -> PostSummary -> List String
detailTags config post =
    case config.detail of
        Just detail ->
            if detail.id == post.id then
                detail.tags

            else
                post.tags

        Nothing ->
            post.tags


identitySection : Config msg -> PostSummary -> Html msg
identitySection config post =
    let
        ratio =
            if post.height > 0 then
                toFloat post.width / toFloat post.height

            else
                1
    in
    div [ class "panel-section inspector-identity" ]
        [ div [ class "inspector-preview", style "aspect-ratio" (String.fromFloat ratio) ]
            [ Ui.Media.image [ class "inspector-preview-image" ]
                { url = Api.Post.mediaUrl config.apiBase post.previewUrl
                , postId = post.id
                , missing = Set.member post.id config.missing
                , onError = config.onMediaError
                }
            ]
        , p [ class "inspector-id" ] [ text ("Post #" ++ post.id) ]
        , dl [ class "facts" ]
            [ fact "Dimensions" (String.fromInt post.width ++ " × " ++ String.fromInt post.height)
            , fact "Aspect" (aspect post)
            , fact "Media type" post.mediaType
            , div [ class "fact" ]
                [ dt [] [ text "Original" ]
                , dd [] [ a [ href (Api.Post.mediaUrl config.apiBase post.originalUrl), target "_blank", rel "noopener" ] [ text "Open" ] ]
                ]
            ]
        ]


selectionSection : Config msg -> Html msg
selectionSection config =
    Panel.section "Selection"
        ([ Feature.Selection.view
            { count = config.selectedCount
            , activeId = Maybe.map .id config.active
            , onClear = config.onClear
            , onApiPending = config.onApiPending
            }
         ]
            ++ (if config.selectedCount > 1 then
                    [ p [ class "panel-label" ] [ text "Common tags" ]
                    , if List.isEmpty config.commonTags then
                        p [ class "panel-note" ] [ text "None in common." ]

                      else
                        ul [ class "tag-list" ] (List.map (tagItem config) config.commonTags)
                    ]

                else
                    []
               )
        )


tagList : Config msg -> List String -> List (Html msg)
tagList config tags =
    if List.isEmpty tags then
        [ p [ class "panel-note" ] [ text "No tags available." ] ]

    else
        [ ul [ class "tag-list" ] (List.map (tagItem config) tags)
        , p [ class "panel-note" ] [ text "Click to filter by a tag; Alt+click to exclude it." ]
        ]


tagItem : Config msg -> String -> Html msg
tagItem config tag =
    li []
        [ button
            [ class "chip"
            , type_ "button"
            , title ("Filter by " ++ tag ++ " (Alt+click to exclude)")
            , on "click" (Decode.map (config.onTag tag) (Decode.field "altKey" Decode.bool))
            ]
            [ text tag ]
        ]


fact : String -> String -> Html msg
fact label value =
    div [ class "fact" ] [ dt [] [ text label ], dd [] [ text value ] ]


aspect : PostSummary -> String
aspect post =
    let
        hundredths =
            if post.height > 0 then
                round (toFloat post.width / toFloat post.height * 100)

            else
                100

        frac =
            modBy 100 hundredths
    in
    String.fromInt (hundredths // 100)
        ++ "."
        ++ (if frac < 10 then
                "0"

            else
                ""
           )
        ++ String.fromInt frac
        ++ " : 1"
